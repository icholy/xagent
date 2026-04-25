package apiserver

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/store"
)

// AtlassianIntegration provides Atlassian-related operations needed by the API server.
type AtlassianIntegration interface {
	UnlinkAccount(ctx context.Context, userID string) error
	GetWebhookSecret(ctx context.Context, orgID int64) (string, error)
	WebhookURL(orgID int64) string
	GenerateWebhookSecret(ctx context.Context, orgID int64) (string, error)
}

// GitHubIntegration provides GitHub-related operations needed by the API server.
type GitHubIntegration interface {
	AppInstallURL() string
}

type Server struct {
	xagentv1connect.UnimplementedXAgentServiceHandler
	log       *slog.Logger
	store     *store.Store
	baseURL   string
	publisher pubsub.Publisher
	atlassian AtlassianIntegration
	github    GitHubIntegration
}

type Options struct {
	Log       *slog.Logger
	Store     *store.Store
	BaseURL   string
	Publisher pubsub.Publisher
	Atlassian AtlassianIntegration
	GitHub    GitHubIntegration
}

func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		log:       log,
		store:     opts.Store,
		baseURL:   opts.BaseURL,
		publisher: opts.Publisher,
		atlassian: opts.Atlassian,
		github:    opts.GitHub,
	}
}

func (s *Server) publish(n model.Notification) {
	if s.publisher == nil {
		return
	}
	if err := s.publisher.Publish(context.Background(), n); err != nil {
		s.log.Warn("failed to publish notification", "error", err, "type", n.Type, "resources", n.Resources)
	}
}

func (s *Server) Ping(ctx context.Context, req *xagentv1.PingRequest) (*xagentv1.PingResponse, error) {
	return &xagentv1.PingResponse{}, nil
}

func (s *Server) GetProfile(ctx context.Context, req *xagentv1.GetProfileRequest) (*xagentv1.GetProfileResponse, error) {
	u := apiauth.Caller(ctx)
	if u == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}
	orgs, err := s.store.ListOrgsByMember(ctx, nil, u.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	user, err := s.store.GetUser(ctx, nil, u.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.GetProfileResponse{
		Profile: &xagentv1.Profile{
			Id:    u.ID,
			Email: u.Email,
			Name:  u.Name,
		},
		DefaultOrgId:     user.DefaultOrgID,
		GithubAccount:    user.GitHubAccountProto(),
		AtlassianAccount: user.AtlassianAccountProto(),
	}
	resp.Orgs = make([]*xagentv1.Org, len(orgs))
	for i, o := range orgs {
		resp.Orgs[i] = o.Proto()
	}
	return resp, nil
}

func (s *Server) ListTasks(ctx context.Context, req *xagentv1.ListTasksRequest) (*xagentv1.ListTasksResponse, error) {
	caller := apiauth.MustCaller(ctx)
	tasks, err := s.store.ListTasks(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListTasksResponse{
		Tasks: make([]*xagentv1.Task, len(tasks)),
	}
	for i, t := range tasks {
		resp.Tasks[i] = t.Proto(s.baseURL)
	}
	return resp, nil
}

func (s *Server) ListRunnerTasks(ctx context.Context, req *xagentv1.ListRunnerTasksRequest) (*xagentv1.ListRunnerTasksResponse, error) {
	caller := apiauth.MustCaller(ctx)
	tasks, err := s.store.ListTasksForRunner(ctx, nil, req.Runner, caller.OrgID)
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
		resp.Tasks[i] = t.Proto(s.baseURL)
	}
	return resp, nil
}

func (s *Server) ListChildTasks(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
	caller := apiauth.MustCaller(ctx)
	tasks, err := s.store.ListTaskChildren(ctx, nil, req.ParentId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListChildTasksResponse{
		Tasks: make([]*xagentv1.Task, len(tasks)),
	}
	for i, t := range tasks {
		resp.Tasks[i] = t.Proto(s.baseURL)
	}
	return resp, nil
}

func (s *Server) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Verify parent task ownership if specified
	if req.Parent != 0 {
		ok, err := s.store.HasTask(ctx, nil, req.Parent, caller.OrgID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if !ok {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("parent task %d not found", req.Parent))
		}
	}
	// Verify runner and workspace exist
	ok, err := s.store.HasWorkspace(ctx, nil, req.Runner, req.Workspace, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("workspace %q not found on runner %q", req.Workspace, req.Runner))
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
		OrgID:        caller.OrgID,
	}
	err = s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.store.CreateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  task.ID,
			Type:    "audit",
			Content: fmt.Sprintf("%s created task", caller.AuditName()),
		}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task created", "id", task.ID, "runner", task.Runner, "workspace", task.Workspace, "org_id", task.OrgID)
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "created", Type: "task", ID: task.ID},
			{Action: "appended", Type: "task_logs", ID: task.ID},
		},
		OrgID: caller.OrgID,
		Time:  time.Now(),
	})
	return &xagentv1.CreateTaskResponse{
		Task: task.Proto(s.baseURL),
	}, nil
}

func (s *Server) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	task, err := s.store.GetTask(ctx, nil, req.Id, caller.OrgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.GetTaskResponse{
		Task: task.Proto(s.baseURL),
	}, nil
}

func (s *Server) GetTaskDetails(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	task, err := s.store.GetTask(ctx, nil, req.Id, caller.OrgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	children, _ := s.store.ListTaskChildren(ctx, nil, req.Id, caller.OrgID)
	events, _ := s.store.ListEventsByTask(ctx, nil, req.Id, caller.OrgID)
	links, _ := s.store.ListLinksByTask(ctx, nil, req.Id, caller.OrgID)
	resp := &xagentv1.GetTaskDetailsResponse{
		Task:     task.Proto(s.baseURL),
		Children: make([]*xagentv1.Task, len(children)),
		Events:   make([]*xagentv1.Event, len(events)),
		Links:    make([]*xagentv1.TaskLink, len(links)),
	}
	for i, c := range children {
		resp.Children[i] = c.Proto(s.baseURL)
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
	caller := apiauth.MustCaller(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		var changed []string
		if req.Name != "" {
			task.Name = req.Name
			changed = append(changed, "name")
		}
		for _, inst := range req.AddInstructions {
			task.Instructions = append(task.Instructions, model.InstructionFromProto(inst))
			changed = append(changed, "instructions")
		}
		if req.Start {
			task.Start()
			changed = append(changed, "status")
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  req.Id,
			Type:    "audit",
			Content: fmt.Sprintf("%s updated task: %s", caller.AuditName(), strings.Join(changed, ", ")),
		}); err != nil {
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
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: req.Id},
			{Action: "appended", Type: "task_logs", ID: req.Id},
		},
		OrgID: caller.OrgID,
		Time:  time.Now(),
	})
	return &xagentv1.UpdateTaskResponse{}, nil
}

func (s *Server) ArchiveTask(ctx context.Context, req *xagentv1.ArchiveTaskRequest) (*xagentv1.ArchiveTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !task.Archive() {
			return fmt.Errorf("cannot archive task with status %s", task.Status)
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  req.Id,
			Type:    "audit",
			Content: fmt.Sprintf("%s archived task", caller.AuditName()),
		}); err != nil {
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
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "archived", Type: "task", ID: req.Id},
			{Action: "appended", Type: "task_logs", ID: req.Id},
		},
		OrgID: caller.OrgID,
		Time:  time.Now(),
	})
	return &xagentv1.ArchiveTaskResponse{}, nil
}

func (s *Server) UnarchiveTask(ctx context.Context, req *xagentv1.UnarchiveTaskRequest) (*xagentv1.UnarchiveTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !task.Unarchive() {
			return fmt.Errorf("cannot unarchive task: not archived")
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  req.Id,
			Type:    "audit",
			Content: fmt.Sprintf("%s unarchived task", caller.AuditName()),
		}); err != nil {
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
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "unarchived", Type: "task", ID: req.Id},
			{Action: "appended", Type: "task_logs", ID: req.Id},
		},
		OrgID: caller.OrgID,
		Time:  time.Now(),
	})
	return &xagentv1.UnarchiveTaskResponse{}, nil
}

func (s *Server) CancelTask(ctx context.Context, req *xagentv1.CancelTaskRequest) (*xagentv1.CancelTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !task.Cancel() {
			return fmt.Errorf("cannot cancel task with status %s", task.Status)
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  req.Id,
			Type:    "audit",
			Content: fmt.Sprintf("%s cancelled task", caller.AuditName()),
		}); err != nil {
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
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "cancelled", Type: "task", ID: req.Id},
			{Action: "appended", Type: "task_logs", ID: req.Id},
		},
		OrgID: caller.OrgID,
		Time:  time.Now(),
	})
	return &xagentv1.CancelTaskResponse{}, nil
}

func (s *Server) RestartTask(ctx context.Context, req *xagentv1.RestartTaskRequest) (*xagentv1.RestartTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
		if err != nil {
			return err
		}
		if !task.Restart() {
			return fmt.Errorf("cannot restart task with status %s", task.Status)
		}
		if err := s.store.UpdateTask(ctx, tx, task); err != nil {
			return err
		}
		if err := s.store.CreateLog(ctx, tx, &model.Log{
			TaskID:  req.Id,
			Type:    "audit",
			Content: fmt.Sprintf("%s restarted task", caller.AuditName()),
		}); err != nil {
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
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "restarted", Type: "task", ID: req.Id},
			{Action: "appended", Type: "task_logs", ID: req.Id},
		},
		OrgID: caller.OrgID,
		Time:  time.Now(),
	})
	return &xagentv1.RestartTaskResponse{}, nil
}

func (s *Server) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, caller.OrgID)
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
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "appended", Type: "task_logs", ID: req.TaskId}},
		OrgID:     caller.OrgID,
		Time:      time.Now(),
	})
	return &xagentv1.UploadLogsResponse{}, nil
}

func (s *Server) ListLogs(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	logs, err := s.store.ListLogsByTask(ctx, nil, req.TaskId, caller.OrgID)
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
	caller := apiauth.MustCaller(ctx)
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, caller.OrgID)
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
		Subscribe: req.Subscribe,
		CreatedAt: time.Now(),
	}
	if err := s.store.CreateLink(ctx, nil, link); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("link created", "task", req.TaskId, "relevance", req.Relevance, "url", req.Url)
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "created", Type: "task_links", ID: req.TaskId},
			{Action: "created", Type: "link", ID: link.ID},
		},
		OrgID: caller.OrgID,
		Time:  time.Now(),
	})
	return &xagentv1.CreateLinkResponse{
		Link: link.Proto(),
	}, nil
}

func (s *Server) ListLinks(ctx context.Context, req *xagentv1.ListLinksRequest) (*xagentv1.ListLinksResponse, error) {
	caller := apiauth.MustCaller(ctx)
	links, err := s.store.ListLinksByTask(ctx, nil, req.TaskId, caller.OrgID)
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
	caller := apiauth.MustCaller(ctx)
	links, err := s.store.FindLinksByURL(ctx, nil, req.Url, caller.OrgID)
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
	caller := apiauth.MustCaller(ctx)
	events, err := s.store.ListEvents(ctx, nil, limit, caller.OrgID)
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
	caller := apiauth.MustCaller(ctx)
	event := &model.Event{
		Description: req.Description,
		Data:        req.Data,
		URL:         req.Url,
		OrgID:       caller.OrgID,
	}
	if err := s.store.CreateEvent(ctx, nil, event); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event created", "id", event.ID, "description", event.Description)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "created", Type: "event", ID: event.ID}},
		OrgID:     caller.OrgID,
		Time:      time.Now(),
	})
	return &xagentv1.CreateEventResponse{
		Event: event.Proto(),
	}, nil
}

func (s *Server) GetEvent(ctx context.Context, req *xagentv1.GetEventRequest) (*xagentv1.GetEventResponse, error) {
	caller := apiauth.MustCaller(ctx)
	event, err := s.store.GetEvent(ctx, nil, req.Id, caller.OrgID)
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
	caller := apiauth.MustCaller(ctx)
	if err := s.store.DeleteEvent(ctx, nil, req.Id, caller.OrgID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event deleted", "id", req.Id)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "deleted", Type: "event", ID: req.Id}},
		OrgID:     caller.OrgID,
		Time:      time.Now(),
	})
	return &xagentv1.DeleteEventResponse{}, nil
}

func (s *Server) AddEventTask(ctx context.Context, req *xagentv1.AddEventTaskRequest) (*xagentv1.AddEventTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
	}
	// Verify event ownership
	ok, err = s.store.HasEvent(ctx, nil, req.EventId, caller.OrgID)
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
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: req.TaskId},
			{Action: "updated", Type: "event", ID: req.EventId},
		},
		OrgID: caller.OrgID,
		Time:  time.Now(),
	})
	return &xagentv1.AddEventTaskResponse{}, nil
}

func (s *Server) RemoveEventTask(ctx context.Context, req *xagentv1.RemoveEventTaskRequest) (*xagentv1.RemoveEventTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
	}
	// Verify event ownership
	ok, err = s.store.HasEvent(ctx, nil, req.EventId, caller.OrgID)
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
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "updated", Type: "task", ID: req.TaskId},
			{Action: "updated", Type: "event", ID: req.EventId},
		},
		OrgID: caller.OrgID,
		Time:  time.Now(),
	})
	return &xagentv1.RemoveEventTaskResponse{}, nil
}

func (s *Server) ListEventTasks(ctx context.Context, req *xagentv1.ListEventTasksRequest) (*xagentv1.ListEventTasksResponse, error) {
	caller := apiauth.MustCaller(ctx)
	taskIDs, err := s.store.ListEventTasks(ctx, nil, req.EventId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListEventTasksResponse{TaskIds: taskIDs}, nil
}

func (s *Server) ListEventsByTask(ctx context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
	caller := apiauth.MustCaller(ctx)
	events, err := s.store.ListEventsByTask(ctx, nil, req.TaskId, caller.OrgID)
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

func (s *Server) SubmitRunnerEvents(ctx context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	for _, pbEvent := range req.Events {
		event := model.RunnerEventFromProto(pbEvent)
		var task *model.Task
		var applied bool
		err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
			var err error
			task, err = s.store.GetTaskForUpdate(ctx, tx, event.TaskID, caller.OrgID)
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
		if applied {
			s.publish(model.Notification{
				Type: "change",
				Resources: []model.NotificationResource{
					{Action: "updated", Type: "task", ID: event.TaskID},
					{Action: "appended", Type: "task_logs", ID: event.TaskID},
				},
				OrgID: caller.OrgID,
				Time:  time.Now(),
			})
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
	caller := apiauth.MustCaller(ctx)
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.store.DeleteWorkspacesByRunner(ctx, tx, req.RunnerId, caller.OrgID); err != nil {
			return err
		}
		for _, ws := range req.Workspaces {
			if err := s.store.CreateWorkspace(ctx, tx, req.RunnerId, ws.Name, ws.Description, caller.OrgID); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("workspaces registered", "runner_id", req.RunnerId, "org_id", caller.OrgID, "count", len(req.Workspaces))
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "registered", Type: "workspaces"}},
		OrgID:     caller.OrgID,
		Time:      time.Now(),
	})
	return &xagentv1.RegisterWorkspacesResponse{}, nil
}

func (s *Server) ListWorkspaces(ctx context.Context, req *xagentv1.ListWorkspacesRequest) (*xagentv1.ListWorkspacesResponse, error) {
	caller := apiauth.MustCaller(ctx)
	workspaces, err := s.store.ListWorkspaces(ctx, nil, caller.OrgID)
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
	caller := apiauth.MustCaller(ctx)
	if req.RunnerId != "" {
		if err := s.store.DeleteWorkspacesByRunner(ctx, nil, req.RunnerId, caller.OrgID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		s.log.Info("workspaces cleared", "org_id", caller.OrgID, "runner", req.RunnerId)
	} else {
		if err := s.store.ClearWorkspaces(ctx, nil, caller.OrgID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		s.log.Info("workspaces cleared", "org_id", caller.OrgID)
	}
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "cleared", Type: "workspaces"}},
		OrgID:     caller.OrgID,
		Time:      time.Now(),
	})
	return &xagentv1.ClearWorkspacesResponse{}, nil
}

func (s *Server) CreateKey(ctx context.Context, req *xagentv1.CreateKeyRequest) (*xagentv1.CreateKeyResponse, error) {
	caller := apiauth.MustCaller(ctx)
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
		OrgID:     caller.OrgID,
		ExpiresAt: expiresAt,
	}
	if err := s.store.CreateKey(ctx, nil, key); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("key created", "id", key.ID, "org_id", caller.OrgID)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "created", Type: "keys"}},
		OrgID:     caller.OrgID,
		Time:      time.Now(),
	})
	return &xagentv1.CreateKeyResponse{
		Key:      key.Proto(),
		RawToken: rawKey,
	}, nil
}

func (s *Server) ListKeys(ctx context.Context, req *xagentv1.ListKeysRequest) (*xagentv1.ListKeysResponse, error) {
	caller := apiauth.MustCaller(ctx)
	keys, err := s.store.ListKeys(ctx, nil, caller.OrgID)
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
	caller := apiauth.MustCaller(ctx)
	if err := s.store.DeleteKey(ctx, nil, req.Id, caller.OrgID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("key deleted", "id", req.Id)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "deleted", Type: "keys"}},
		OrgID:     caller.OrgID,
		Time:      time.Now(),
	})
	return &xagentv1.DeleteKeyResponse{}, nil
}

func (s *Server) UnlinkGitHubAccount(ctx context.Context, req *xagentv1.UnlinkGitHubAccountRequest) (*xagentv1.UnlinkGitHubAccountResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if err := s.store.UnlinkGitHubAccount(ctx, nil, caller.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("github account unlinked", "owner", caller.ID)
	return &xagentv1.UnlinkGitHubAccountResponse{}, nil
}

func (s *Server) UnlinkAtlassianAccount(ctx context.Context, req *xagentv1.UnlinkAtlassianAccountRequest) (*xagentv1.UnlinkAtlassianAccountResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if err := s.atlassian.UnlinkAccount(ctx, caller.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("atlassian account unlinked", "owner", caller.ID)
	return &xagentv1.UnlinkAtlassianAccountResponse{}, nil
}

func (s *Server) CreateOrg(ctx context.Context, req *xagentv1.CreateOrgRequest) (*xagentv1.CreateOrgResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if req.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}
	org := &model.Org{
		Name:  req.Name,
		Owner: caller.ID,
	}
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.store.CreateOrg(ctx, tx, org); err != nil {
			return err
		}
		if err := s.store.AddOrgMember(ctx, tx, &model.OrgMember{
			OrgID:  org.ID,
			UserID: caller.ID,
			Role:   "owner",
		}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("org created", "id", org.ID, "name", org.Name, "owner", caller.ID)
	return &xagentv1.CreateOrgResponse{Org: org.Proto()}, nil
}

func (s *Server) ListOrgs(ctx context.Context, req *xagentv1.ListOrgsRequest) (*xagentv1.ListOrgsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	orgs, err := s.store.ListOrgsByMember(ctx, nil, caller.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	pbOrgs := make([]*xagentv1.Org, len(orgs))
	for i, o := range orgs {
		pbOrgs[i] = o.Proto()
	}
	return &xagentv1.ListOrgsResponse{Orgs: pbOrgs}, nil
}

func (s *Server) DeleteOrg(ctx context.Context, req *xagentv1.DeleteOrgRequest) (*xagentv1.DeleteOrgResponse, error) {
	caller := apiauth.MustCaller(ctx)
	org, err := s.store.GetOrg(ctx, nil, req.Id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("org not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if org.Owner != caller.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the org owner can delete it"))
	}
	user, err := s.store.GetUser(ctx, nil, caller.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if user.DefaultOrgID == req.Id {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("cannot delete your default org"))
	}
	if err := s.store.ArchiveOrg(ctx, nil, req.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("org archived", "id", req.Id, "owner", caller.ID)
	return &xagentv1.DeleteOrgResponse{}, nil
}

func (s *Server) AddOrgMember(ctx context.Context, req *xagentv1.AddOrgMemberRequest) (*xagentv1.AddOrgMemberResponse, error) {
	caller := apiauth.MustCaller(ctx)
	org, err := s.store.GetOrg(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if org.Owner != caller.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the org owner can add members"))
	}
	if req.Email == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("email is required"))
	}
	user, err := s.store.GetUserByEmail(ctx, nil, req.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no user found with email %q", req.Email))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	member := &model.OrgMember{
		OrgID:  caller.OrgID,
		UserID: user.ID,
		Role:   "member",
	}
	if err := s.store.AddOrgMember(ctx, nil, member); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("org member added", "org_id", caller.OrgID, "user_id", user.ID, "email", req.Email)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "added", Type: "org_members"}},
		OrgID:     caller.OrgID,
		Time:      time.Now(),
	})
	return &xagentv1.AddOrgMemberResponse{
		Member: &xagentv1.OrgMember{
			OrgId:  member.OrgID,
			UserId: member.UserID,
			Email:  user.Email,
			Name:   user.Name,
			Role:   member.Role,
		},
	}, nil
}

func (s *Server) RemoveOrgMember(ctx context.Context, req *xagentv1.RemoveOrgMemberRequest) (*xagentv1.RemoveOrgMemberResponse, error) {
	caller := apiauth.MustCaller(ctx)
	org, err := s.store.GetOrg(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if org.Owner != caller.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the org owner can remove members"))
	}
	if req.UserId == org.Owner {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cannot remove the org owner"))
	}
	if err := s.store.RemoveOrgMember(ctx, nil, caller.OrgID, req.UserId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("org member removed", "org_id", caller.OrgID, "user_id", req.UserId)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "removed", Type: "org_members"}},
		OrgID:     caller.OrgID,
		Time:      time.Now(),
	})
	return &xagentv1.RemoveOrgMemberResponse{}, nil
}

func (s *Server) ListOrgMembers(ctx context.Context, req *xagentv1.ListOrgMembersRequest) (*xagentv1.ListOrgMembersResponse, error) {
	caller := apiauth.MustCaller(ctx)
	members, err := s.store.ListOrgMembersWithUsers(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	pbMembers := make([]*xagentv1.OrgMember, len(members))
	for i, m := range members {
		pbMembers[i] = m.Proto()
	}
	return &xagentv1.ListOrgMembersResponse{Members: pbMembers}, nil
}

func (s *Server) GetOrgSettings(ctx context.Context, req *xagentv1.GetOrgSettingsRequest) (*xagentv1.GetOrgSettingsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	resp := &xagentv1.GetOrgSettingsResponse{
		McpUrl: s.baseURL + "/mcp",
	}
	if s.atlassian != nil {
		secret, err := s.atlassian.GetWebhookSecret(ctx, caller.OrgID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		resp.AtlassianWebhookSecret = secret
		resp.AtlassianWebhookUrl = s.atlassian.WebhookURL(caller.OrgID)
	}
	if s.github != nil {
		resp.GithubAppUrl = s.github.AppInstallURL()
	}
	return resp, nil
}

func (s *Server) GenerateAtlassianWebhookSecret(ctx context.Context, req *xagentv1.GenerateAtlassianWebhookSecretRequest) (*xagentv1.GenerateAtlassianWebhookSecretResponse, error) {
	caller := apiauth.MustCaller(ctx)
	secret, err := s.atlassian.GenerateWebhookSecret(ctx, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.GenerateAtlassianWebhookSecretResponse{
		Secret:     secret,
		WebhookUrl: s.atlassian.WebhookURL(caller.OrgID),
	}, nil
}

func (s *Server) GetRoutingRules(ctx context.Context, req *xagentv1.GetRoutingRulesRequest) (*xagentv1.GetRoutingRulesResponse, error) {
	caller := apiauth.MustCaller(ctx)
	rules, err := s.store.GetOrgRoutingRules(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	pb := make([]*xagentv1.RoutingRule, len(rules))
	for i := range rules {
		pb[i] = rules[i].Proto()
	}
	return &xagentv1.GetRoutingRulesResponse{Rules: pb}, nil
}

func (s *Server) SetRoutingRules(ctx context.Context, req *xagentv1.SetRoutingRulesRequest) (*xagentv1.SetRoutingRulesResponse, error) {
	caller := apiauth.MustCaller(ctx)
	rules := make([]model.RoutingRule, len(req.Rules))
	for i, r := range req.Rules {
		rules[i] = model.RoutingRuleFromProto(r)
	}
	if err := s.store.SetOrgRoutingRules(ctx, nil, caller.OrgID, rules); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	pb := make([]*xagentv1.RoutingRule, len(rules))
	for i := range rules {
		pb[i] = rules[i].Proto()
	}
	return &xagentv1.SetRoutingRulesResponse{Rules: pb}, nil
}
