package server

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"github.com/google/go-github/v68/github"
	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/deviceauth"
	"github.com/icholy/xagent/internal/ghauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/store"
	"github.com/justinas/alice"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type GitHubConfig struct {
	AppID         string
	ClientID      string
	ClientSecret  string
	WebhookSecret string
}

type Server struct {
	xagentv1connect.UnimplementedXAgentServiceHandler
	log           *slog.Logger
	store         *store.Store
	auth          *apiauth.Auth
	discovery     deviceauth.DiscoveryConfig
	github        *GitHubConfig
	baseURL       string
	encryptionKey []byte
}

type Options struct {
	Log           *slog.Logger
	Store         *store.Store
	Auth          *apiauth.Auth
	Discovery     deviceauth.DiscoveryConfig
	GitHub        *GitHubConfig
	BaseURL       string
	EncryptionKey []byte
}

func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		log:           log,
		store:         opts.Store,
		auth:          opts.Auth,
		discovery:     opts.Discovery,
		github:        opts.GitHub,
		baseURL:       opts.BaseURL,
		encryptionKey: opts.EncryptionKey,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// Device flow discovery endpoint (public)
	mux.HandleFunc(deviceauth.DiscoveryPath, s.handleDeviceConfig)
	// Auth routes (login, callback, logout)
	mux.Handle("/auth/", s.auth.Handler())
	// Connect RPC API (protected)
	// HTTP middleware checks auth and attaches UserInfo to context
	// Connect interceptor enforces auth with proper RPC error responses
	otelInterceptor, err := otelconnect.NewInterceptor()
	if err != nil {
		s.log.Error("failed to create otelconnect interceptor", "error", err)
	}
	path, handler := xagentv1connect.NewXAgentServiceHandler(s,
		connect.WithInterceptors(otelInterceptor, apiauth.RequireUserInterceptor()),
	)
	mux.Handle(path, alice.New(s.auth.CheckAuth(), s.auth.AttachUserInfo()).Then(handler))
	// GitHub App routes (conditionally registered)
	if s.github != nil {
		gh := ghauth.New(ghauth.Config{
			ClientID:     s.github.ClientID,
			ClientSecret: s.github.ClientSecret,
			RedirectURL:  s.baseURL + "/github/callback",
			Log:          s.log,
			OnSuccess: func(w http.ResponseWriter, r *http.Request, ghUser *github.User) {
				user := apiauth.User(r.Context())
				if user == nil {
					http.Error(w, "not authenticated", http.StatusUnauthorized)
					return
				}
				account := &model.GitHubAccount{
					Owner:          user.ID,
					GitHubUserID:   ghUser.GetID(),
					GitHubUsername: ghUser.GetLogin(),
				}
				if err := s.store.CreateGitHubAccount(r.Context(), nil, account); err != nil {
					http.Error(w, "failed to save GitHub account", http.StatusInternalServerError)
					return
				}
				http.Redirect(w, r, "/ui/settings", http.StatusFound)
			},
		})
		mux.Handle("/github/", s.auth.RequireAuth()(http.StripPrefix("/github", gh)))
		mux.HandleFunc("/webhook/github", s.handleGitHubWebhook)
	}
	// React UI (SPA with client-side routing, protected by cookie auth)
	mux.Handle("/ui/", http.StripPrefix("/ui", s.auth.RequireAuth()(WebUI())))
	mux.Handle("/", http.RedirectHandler("/ui/", http.StatusFound))
	return otelhttp.NewHandler(mux, "xagent")
}

func (s *Server) handleDeviceConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.discovery)
}

// userID returns the authenticated user's ID from context.
func (s *Server) userID(ctx context.Context) string {
	u := apiauth.User(ctx)
	if u == nil {
		panic("no UserInfo in request context")
	}
	return u.ID
}

func (s *Server) Ping(ctx context.Context, req *xagentv1.PingRequest) (*xagentv1.PingResponse, error) {
	return &xagentv1.PingResponse{}, nil
}

func (s *Server) GetProfile(ctx context.Context, req *xagentv1.GetProfileRequest) (*xagentv1.GetProfileResponse, error) {
	u := apiauth.User(ctx)
	if u == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}
	return &xagentv1.GetProfileResponse{
		Profile: &xagentv1.Profile{
			Id:    u.ID,
			Email: u.Email,
			Name:  u.Name,
		},
	}, nil
}

func (s *Server) ListTasks(ctx context.Context, req *xagentv1.ListTasksRequest) (*xagentv1.ListTasksResponse, error) {
	userID := s.userID(ctx)
	tasks, err := s.store.ListTasks(ctx, nil, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListTasksResponse{
		Tasks: make([]*xagentv1.Task, len(tasks)),
	}
	for i, t := range tasks {
		resp.Tasks[i] = t.Proto()
	}
	return resp, nil
}

func (s *Server) ListRunnerTasks(ctx context.Context, req *xagentv1.ListRunnerTasksRequest) (*xagentv1.ListRunnerTasksResponse, error) {
	userID := s.userID(ctx)
	tasks, err := s.store.ListTasksForRunner(ctx, nil, req.Runner, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &xagentv1.ListRunnerTasksResponse{}, nil
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListRunnerTasksResponse{
		Tasks: make([]*xagentv1.Task, len(tasks)),
	}
	for i, t := range tasks {
		resp.Tasks[i] = t.Proto()
	}
	return resp, nil
}

func (s *Server) ListChildTasks(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
	userID := s.userID(ctx)
	tasks, err := s.store.ListTaskChildren(ctx, nil, req.ParentId, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListChildTasksResponse{
		Tasks: make([]*xagentv1.Task, len(tasks)),
	}
	for i, t := range tasks {
		resp.Tasks[i] = t.Proto()
	}
	return resp, nil
}

func (s *Server) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	userID := s.userID(ctx)
	// Verify parent task ownership if specified
	if req.Parent != 0 {
		ok, err := s.store.HasTask(ctx, nil, req.Parent, userID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if !ok {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("parent task %d not found", req.Parent))
		}
	}
	instructions := make([]model.Instruction, len(req.Instructions))
	for i, inst := range req.Instructions {
		instructions[i] = model.InstructionFromProto(inst)
	}
	task := &model.Task{
		Name:         req.Name,
		Parent:       req.Parent,
		Runner:       req.Runner,
		Workspace:    req.Workspace,
		Instructions: instructions,
		Status:       model.TaskStatusPending,
		Command:      model.TaskCommandStart,
		Version:      1,
		Owner:        userID,
	}
	if err := s.store.CreateTask(ctx, nil, task); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task created", "id", task.ID, "runner", task.Runner, "workspace", task.Workspace, "owner", task.Owner)
	return &xagentv1.CreateTaskResponse{
		Task: task.Proto(),
	}, nil
}

func (s *Server) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	userID := s.userID(ctx)
	task, err := s.store.GetTask(ctx, nil, req.Id, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.GetTaskResponse{
		Task: task.Proto(),
	}, nil
}

func (s *Server) GetTaskDetails(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
	userID := s.userID(ctx)
	task, err := s.store.GetTask(ctx, nil, req.Id, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	children, _ := s.store.ListTaskChildren(ctx, nil, req.Id, userID)
	events, _ := s.store.ListEventsByTask(ctx, nil, req.Id, userID)
	links, _ := s.store.ListLinksByTask(ctx, nil, req.Id, userID)
	resp := &xagentv1.GetTaskDetailsResponse{
		Task:     task.Proto(),
		Children: make([]*xagentv1.Task, len(children)),
		Events:   make([]*xagentv1.Event, len(events)),
		Links:    make([]*xagentv1.TaskLink, len(links)),
	}
	for i, c := range children {
		resp.Children[i] = c.Proto()
	}
	for i, e := range events {
		resp.Events[i] = e.Proto()
	}
	for i, l := range links {
		resp.Links[i] = l.Proto()
	}
	return resp, nil
}

func (s *Server) UpdateTask(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
	userID := s.userID(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTask(ctx, tx, req.Id, userID)
		if err != nil {
			return err
		}
		if req.Name != "" {
			task.Name = req.Name
		}
		for _, inst := range req.AddInstructions {
			task.Instructions = append(task.Instructions, model.InstructionFromProto(inst))
		}
		if req.Start {
			task.Start()
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task updated", "id", req.Id, "name", req.Name, "start", req.Start, "instructions_added", len(req.AddInstructions))
	return &xagentv1.UpdateTaskResponse{}, nil
}

func (s *Server) DeleteTask(ctx context.Context, req *xagentv1.DeleteTaskRequest) (*xagentv1.DeleteTaskResponse, error) {
	userID := s.userID(ctx)
	if err := s.store.DeleteTask(ctx, nil, req.Id, userID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task deleted", "id", req.Id)
	return &xagentv1.DeleteTaskResponse{}, nil
}

func (s *Server) ArchiveTask(ctx context.Context, req *xagentv1.ArchiveTaskRequest) (*xagentv1.ArchiveTaskResponse, error) {
	userID := s.userID(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTask(ctx, tx, req.Id, userID)
		if err != nil {
			return err
		}
		if !task.Archive() {
			return fmt.Errorf("cannot archive task with status %s", task.Status)
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task archived", "id", req.Id)
	return &xagentv1.ArchiveTaskResponse{}, nil
}

func (s *Server) UnarchiveTask(ctx context.Context, req *xagentv1.UnarchiveTaskRequest) (*xagentv1.UnarchiveTaskResponse, error) {
	userID := s.userID(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTask(ctx, tx, req.Id, userID)
		if err != nil {
			return err
		}
		if !task.Unarchive() {
			return fmt.Errorf("cannot unarchive task: not archived")
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task unarchived", "id", req.Id)
	return &xagentv1.UnarchiveTaskResponse{}, nil
}

func (s *Server) CancelTask(ctx context.Context, req *xagentv1.CancelTaskRequest) (*xagentv1.CancelTaskResponse, error) {
	userID := s.userID(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTask(ctx, tx, req.Id, userID)
		if err != nil {
			return err
		}
		if !task.Cancel() {
			return fmt.Errorf("cannot cancel task with status %s", task.Status)
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task cancelled", "id", req.Id)
	return &xagentv1.CancelTaskResponse{}, nil
}

func (s *Server) RestartTask(ctx context.Context, req *xagentv1.RestartTaskRequest) (*xagentv1.RestartTaskResponse, error) {
	userID := s.userID(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTask(ctx, tx, req.Id, userID)
		if err != nil {
			return err
		}
		if !task.Restart() {
			return fmt.Errorf("cannot restart task with status %s", task.Status)
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task restarted", "id", req.Id)
	return &xagentv1.RestartTaskResponse{}, nil
}

func (s *Server) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	userID := s.userID(ctx)
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
	}
	for _, entry := range req.Entries {
		log := model.LogFromProto(entry)
		log.TaskID = req.TaskId
		if err := s.store.CreateLog(ctx, nil, &log); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	return &xagentv1.UploadLogsResponse{}, nil
}

func (s *Server) ListLogs(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
	userID := s.userID(ctx)
	logs, err := s.store.ListLogsByTask(ctx, nil, req.TaskId, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListLogsResponse{
		Entries: make([]*xagentv1.LogEntry, len(logs)),
	}
	for i, l := range logs {
		resp.Entries[i] = l.Proto()
	}
	return resp, nil
}

func (s *Server) CreateLink(ctx context.Context, req *xagentv1.CreateLinkRequest) (*xagentv1.CreateLinkResponse, error) {
	userID := s.userID(ctx)
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
	}
	link := &model.Link{
		TaskID:    req.TaskId,
		Relevance: req.Relevance,
		URL:       req.Url,
		Title:     req.Title,
		Notify:    req.Notify,
		CreatedAt: time.Now(),
	}
	if err := s.store.CreateLink(ctx, nil, link); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("link created", "task", req.TaskId, "relevance", req.Relevance, "url", req.Url)
	return &xagentv1.CreateLinkResponse{
		Link: link.Proto(),
	}, nil
}

func (s *Server) ListLinks(ctx context.Context, req *xagentv1.ListLinksRequest) (*xagentv1.ListLinksResponse, error) {
	userID := s.userID(ctx)
	links, err := s.store.ListLinksByTask(ctx, nil, req.TaskId, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListLinksResponse{
		Links: make([]*xagentv1.TaskLink, len(links)),
	}
	for i, l := range links {
		resp.Links[i] = l.Proto()
	}
	return resp, nil
}

func (s *Server) FindLinksByURL(ctx context.Context, req *xagentv1.FindLinksByURLRequest) (*xagentv1.FindLinksByURLResponse, error) {
	userID := s.userID(ctx)
	links, err := s.store.FindLinksByURL(ctx, nil, req.Url, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.FindLinksByURLResponse{
		Links: make([]*xagentv1.TaskLink, len(links)),
	}
	for i, l := range links {
		resp.Links[i] = l.Proto()
	}
	return resp, nil
}

func (s *Server) ListEvents(ctx context.Context, req *xagentv1.ListEventsRequest) (*xagentv1.ListEventsResponse, error) {
	const maxLimit = 100
	limit := cmp.Or(int(req.Limit), maxLimit)
	if limit < 0 || limit > maxLimit {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("limit must be at most %d", maxLimit))
	}
	userID := s.userID(ctx)
	events, err := s.store.ListEvents(ctx, nil, limit, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListEventsResponse{
		Events: make([]*xagentv1.Event, len(events)),
	}
	for i, e := range events {
		resp.Events[i] = e.Proto()
	}
	return resp, nil
}

func (s *Server) CreateEvent(ctx context.Context, req *xagentv1.CreateEventRequest) (*xagentv1.CreateEventResponse, error) {
	userID := s.userID(ctx)
	event := &model.Event{
		Description: req.Description,
		Data:        req.Data,
		URL:         req.Url,
		Owner:       userID,
	}
	if err := s.store.CreateEvent(ctx, nil, event); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event created", "id", event.ID, "description", event.Description)
	return &xagentv1.CreateEventResponse{
		Event: event.Proto(),
	}, nil
}

func (s *Server) GetEvent(ctx context.Context, req *xagentv1.GetEventRequest) (*xagentv1.GetEventResponse, error) {
	userID := s.userID(ctx)
	event, err := s.store.GetEvent(ctx, nil, req.Id, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("event %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.GetEventResponse{
		Event: event.Proto(),
	}, nil
}

func (s *Server) DeleteEvent(ctx context.Context, req *xagentv1.DeleteEventRequest) (*xagentv1.DeleteEventResponse, error) {
	userID := s.userID(ctx)
	if err := s.store.DeleteEvent(ctx, nil, req.Id, userID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event deleted", "id", req.Id)
	return &xagentv1.DeleteEventResponse{}, nil
}

func (s *Server) AddEventTask(ctx context.Context, req *xagentv1.AddEventTaskRequest) (*xagentv1.AddEventTaskResponse, error) {
	userID := s.userID(ctx)
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
	}
	// Verify event ownership
	ok, err = s.store.HasEvent(ctx, nil, req.EventId, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("event %d not found", req.EventId))
	}
	if err := s.store.AddEventTask(ctx, nil, req.EventId, req.TaskId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event task added", "event_id", req.EventId, "task_id", req.TaskId)
	return &xagentv1.AddEventTaskResponse{}, nil
}

func (s *Server) RemoveEventTask(ctx context.Context, req *xagentv1.RemoveEventTaskRequest) (*xagentv1.RemoveEventTaskResponse, error) {
	userID := s.userID(ctx)
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
	}
	// Verify event ownership
	ok, err = s.store.HasEvent(ctx, nil, req.EventId, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("event %d not found", req.EventId))
	}
	if err := s.store.RemoveEventTask(ctx, nil, req.EventId, req.TaskId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event task removed", "event_id", req.EventId, "task_id", req.TaskId)
	return &xagentv1.RemoveEventTaskResponse{}, nil
}

func (s *Server) ListEventTasks(ctx context.Context, req *xagentv1.ListEventTasksRequest) (*xagentv1.ListEventTasksResponse, error) {
	userID := s.userID(ctx)
	taskIDs, err := s.store.ListEventTasks(ctx, nil, req.EventId, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListEventTasksResponse{TaskIds: taskIDs}, nil
}

func (s *Server) ListEventsByTask(ctx context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
	userID := s.userID(ctx)
	events, err := s.store.ListEventsByTask(ctx, nil, req.TaskId, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListEventsByTaskResponse{
		Events: make([]*xagentv1.Event, len(events)),
	}
	for i, e := range events {
		resp.Events[i] = e.Proto()
	}
	return resp, nil
}

func (s *Server) ProcessEvent(ctx context.Context, req *xagentv1.ProcessEventRequest) (*xagentv1.ProcessEventResponse, error) {
	userID := s.userID(ctx)
	event, err := s.store.GetEvent(ctx, nil, req.Id, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("event %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	ids, err := s.processEventInternal(ctx, event.ID, event.URL, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ProcessEventResponse{TaskIds: ids}, nil
}

func (s *Server) processEventInternal(ctx context.Context, eventID int64, eventURL string, owner string) ([]int64, error) {
	if eventURL == "" {
		return nil, nil
	}
	links, err := s.store.FindLinksByURL(ctx, nil, eventURL, owner)
	if err != nil {
		return nil, err
	}
	taskIDs := map[int64]bool{}
	for _, link := range links {
		if !link.Notify || taskIDs[link.TaskID] {
			continue
		}
		task, err := s.store.GetTask(ctx, nil, link.TaskID, owner)
		if err != nil {
			s.log.Warn("failed to get task", "task_id", link.TaskID, "error", err)
			continue
		}
		if task.Archived {
			s.log.Info("skipping archived task", "task_id", link.TaskID)
			continue
		}
		taskIDs[link.TaskID] = true
		if err := s.store.AddEventTask(ctx, nil, eventID, link.TaskID); err != nil {
			s.log.Warn("failed to add event task", "event_id", eventID, "task_id", link.TaskID, "error", err)
		}
		err = s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
			task, err := s.store.GetTask(ctx, tx, link.TaskID, owner)
			if err != nil {
				return err
			}
			task.Start()
			if err := s.store.UpdateTask(ctx, tx, task); err != nil {
				return err
			}
			return tx.Commit()
		})
		if err != nil {
			s.log.Warn("failed to start task", "task_id", link.TaskID, "error", err)
		}
	}
	ids := slices.Collect(maps.Keys(taskIDs))
	s.log.Info("event processed", "id", eventID, "tasks_routed", len(ids))
	return ids, nil
}

func (s *Server) SubmitRunnerEvents(ctx context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
	userID := s.userID(ctx)
	for _, pbEvent := range req.Events {
		event := model.RunnerEventFromProto(pbEvent)
		var task *model.Task
		var applied bool
		err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
			var err error
			task, err = s.store.GetTask(ctx, tx, event.TaskID, userID)
			if err != nil {
				return err
			}
			applied = task.ApplyRunnerEvent(&event)
			s.log.Info("runner event recieved",
				"task_id", event.TaskID,
				"event", event.Event,
				"version", event.Version,
				"status", task.Status,
				"applied", applied,
			)
			if !applied {
				return nil
			}
			if err := s.store.UpdateTask(ctx, tx, task); err != nil {
				return err
			}
			if log, ok := s.toRunnerEventLog(event); ok {
				if err := s.store.CreateLog(ctx, tx, &log); err != nil {
					return err
				}
			}
			return tx.Commit()
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", event.TaskID))
			}
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	return &xagentv1.SubmitRunnerEventsResponse{}, nil
}

func (s *Server) toRunnerEventLog(e model.RunnerEvent) (model.Log, bool) {
	switch e.Event {
	case model.RunnerEventStarted:
		return model.Log{
			TaskID:  e.TaskID,
			Type:    "info",
			Content: "container started",
		}, true
	case model.RunnerEventStopped:
		return model.Log{
			TaskID:  e.TaskID,
			Type:    "info",
			Content: "container exited successfully",
		}, true
	case model.RunnerEventFailed:
		return model.Log{
			TaskID:  e.TaskID,
			Type:    "error",
			Content: "container failed",
		}, true
	default:
		return model.Log{}, false
	}
}

func (s *Server) RegisterWorkspaces(ctx context.Context, req *xagentv1.RegisterWorkspacesRequest) (*xagentv1.RegisterWorkspacesResponse, error) {
	userID := s.userID(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.store.DeleteWorkspacesByRunner(ctx, tx, req.RunnerId, userID); err != nil {
			return err
		}
		for _, ws := range req.Workspaces {
			if err := s.store.CreateWorkspace(ctx, tx, req.RunnerId, ws.Name, userID); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("workspaces registered", "runner_id", req.RunnerId, "owner", userID, "count", len(req.Workspaces))
	return &xagentv1.RegisterWorkspacesResponse{}, nil
}

func (s *Server) ListWorkspaces(ctx context.Context, req *xagentv1.ListWorkspacesRequest) (*xagentv1.ListWorkspacesResponse, error) {
	userID := s.userID(ctx)
	workspaces, err := s.store.ListWorkspaces(ctx, nil, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	result := make([]*xagentv1.RegisteredWorkspace, len(workspaces))
	for i, ws := range workspaces {
		result[i] = ws.Proto()
	}
	return &xagentv1.ListWorkspacesResponse{Workspaces: result}, nil
}

func (s *Server) ClearWorkspaces(ctx context.Context, req *xagentv1.ClearWorkspacesRequest) (*xagentv1.ClearWorkspacesResponse, error) {
	userID := s.userID(ctx)
	if req.RunnerId != "" {
		if err := s.store.DeleteWorkspacesByRunner(ctx, nil, req.RunnerId, userID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		s.log.Info("workspaces cleared", "owner", userID, "runner", req.RunnerId)
	} else {
		if err := s.store.ClearWorkspaces(ctx, nil, userID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		s.log.Info("workspaces cleared", "owner", userID)
	}
	return &xagentv1.ClearWorkspacesResponse{}, nil
}

func (s *Server) CreateKey(ctx context.Context, req *xagentv1.CreateKeyRequest) (*xagentv1.CreateKeyResponse, error) {
	userID := s.userID(ctx)
	rawKey, err := apiauth.GenerateKey()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t := req.ExpiresAt.AsTime()
		expiresAt = &t
	}
	key := &model.Key{
		ID:        uuid.NewString(),
		Name:      req.Name,
		TokenHash: apiauth.HashKey(rawKey),
		Owner:     userID,
		ExpiresAt: expiresAt,
	}
	if err := s.store.CreateKey(ctx, nil, key); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("key created", "id", key.ID, "owner", userID)
	return &xagentv1.CreateKeyResponse{
		Key:      key.Proto(),
		RawToken: rawKey,
	}, nil
}

func (s *Server) ListKeys(ctx context.Context, req *xagentv1.ListKeysRequest) (*xagentv1.ListKeysResponse, error) {
	userID := s.userID(ctx)
	keys, err := s.store.ListKeys(ctx, nil, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListKeysResponse{
		Keys: make([]*xagentv1.Key, len(keys)),
	}
	for i, k := range keys {
		resp.Keys[i] = k.Proto()
	}
	return resp, nil
}

func (s *Server) DeleteKey(ctx context.Context, req *xagentv1.DeleteKeyRequest) (*xagentv1.DeleteKeyResponse, error) {
	userID := s.userID(ctx)
	if err := s.store.DeleteKey(ctx, nil, req.Id, userID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("key deleted", "id", req.Id)
	return &xagentv1.DeleteKeyResponse{}, nil
}

func (s *Server) GetGitHubAccount(ctx context.Context, req *xagentv1.GetGitHubAccountRequest) (*xagentv1.GetGitHubAccountResponse, error) {
	userID := s.userID(ctx)
	account, err := s.store.GetGitHubAccountByOwner(ctx, nil, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &xagentv1.GetGitHubAccountResponse{}, nil
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.GetGitHubAccountResponse{Account: account.Proto()}, nil
}

func (s *Server) UnlinkGitHubAccount(ctx context.Context, req *xagentv1.UnlinkGitHubAccountRequest) (*xagentv1.UnlinkGitHubAccountResponse, error) {
	userID := s.userID(ctx)
	if err := s.store.DeleteGitHubAccount(ctx, nil, userID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("github account unlinked", "owner", userID)
	return &xagentv1.UnlinkGitHubAccountResponse{}, nil
}

