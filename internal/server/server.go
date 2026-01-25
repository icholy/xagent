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
	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/deviceauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/notify"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/store"
)

type Server struct {
	xagentv1connect.UnimplementedXAgentServiceHandler
	log        *slog.Logger
	tasks      *store.TaskRepository
	logs       *store.LogRepository
	links      *store.LinkRepository
	events     *store.EventRepository
	workspaces *store.WorkspaceRepository
	notify     bool
	auth       *apiauth.Auth
	discovery  deviceauth.DiscoveryConfig
}

type Options struct {
	Log        *slog.Logger
	Tasks      *store.TaskRepository
	Logs       *store.LogRepository
	Links      *store.LinkRepository
	Events     *store.EventRepository
	Workspaces *store.WorkspaceRepository
	Notify     bool
	Auth       *apiauth.Auth
	Discovery  deviceauth.DiscoveryConfig
}

func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		log:        log,
		tasks:      opts.Tasks,
		logs:       opts.Logs,
		links:      opts.Links,
		events:     opts.Events,
		workspaces: opts.Workspaces,
		notify:     opts.Notify,
		auth:       opts.Auth,
		discovery:  opts.Discovery,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Device flow discovery endpoint (public)
	mux.HandleFunc("/device/config", s.handleDeviceConfig)

	// Auth routes (login, callback, logout)
	mux.Handle("/auth/", s.auth.Handler)

	// Connect RPC API (protected)
	path, handler := xagentv1connect.NewXAgentServiceHandler(s)
	mux.Handle(path, s.auth.RequireAuth()(handler))

	// React UI (SPA with client-side routing, protected by cookie auth)
	mux.Handle("/", s.auth.RequireAuth()(WebUI()))

	return mux
}

func (s *Server) handleDeviceConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.discovery)
}

func (s *Server) ListTasks(ctx context.Context, req *xagentv1.ListTasksRequest) (*xagentv1.ListTasksResponse, error) {
	var tasks []*model.Task
	var err error

	if req.HasCommand {
		tasks, err = s.tasks.ListWithCommand(ctx, nil)
	} else {
		tasks, err = s.tasks.List(ctx, nil)
	}
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

func (s *Server) ListChildTasks(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
	tasks, err := s.tasks.ListChildren(ctx, nil, req.ParentId)
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
	instructions := make([]model.Instruction, len(req.Instructions))
	for i, inst := range req.Instructions {
		instructions[i] = model.InstructionFromProto(inst)
	}

	task := &model.Task{
		Name:         req.Name,
		Parent:       req.Parent,
		Workspace:    req.Workspace,
		Instructions: instructions,
		Status:       model.TaskStatusPending,
		Command:      model.TaskCommandStart,
		Version:      1,
	}

	if err := s.tasks.Create(ctx, nil, task); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.log.Info("task created", "id", task.ID, "workspace", task.Workspace)
	return &xagentv1.CreateTaskResponse{
		Task: task.Proto(),
	}, nil
}

func (s *Server) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	task, err := s.tasks.Get(ctx, nil, req.Id)
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
	task, err := s.tasks.Get(ctx, nil, req.Id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	children, _ := s.tasks.ListChildren(ctx, nil, req.Id)
	events, _ := s.events.ListByTask(ctx, nil, req.Id)
	links, _ := s.links.ListByTask(ctx, nil, req.Id)

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
	err := s.tasks.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.tasks.Get(ctx, tx, req.Id)
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

		if err := s.tasks.Put(ctx, tx, task); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.log.Info("task updated", "id", req.Id, "name", req.Name, "start", req.Start, "instructions_added", len(req.AddInstructions))
	return &xagentv1.UpdateTaskResponse{}, nil
}

func (s *Server) DeleteTask(ctx context.Context, req *xagentv1.DeleteTaskRequest) (*xagentv1.DeleteTaskResponse, error) {
	if err := s.tasks.Delete(ctx, nil, req.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.log.Info("task deleted", "id", req.Id)
	return &xagentv1.DeleteTaskResponse{}, nil
}

func (s *Server) ArchiveTask(ctx context.Context, req *xagentv1.ArchiveTaskRequest) (*xagentv1.ArchiveTaskResponse, error) {
	err := s.tasks.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.tasks.Get(ctx, tx, req.Id)
		if err != nil {
			return err
		}
		if !task.Archive() {
			return fmt.Errorf("cannot archive task with status %s", task.Status)
		}
		if err := s.tasks.Put(ctx, tx, task); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.log.Info("task archived", "id", req.Id)
	return &xagentv1.ArchiveTaskResponse{}, nil
}

func (s *Server) CancelTask(ctx context.Context, req *xagentv1.CancelTaskRequest) (*xagentv1.CancelTaskResponse, error) {
	err := s.tasks.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.tasks.Get(ctx, tx, req.Id)
		if err != nil {
			return err
		}
		if !task.Cancel() {
			return fmt.Errorf("cannot cancel task with status %s", task.Status)
		}
		if err := s.tasks.Put(ctx, tx, task); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.log.Info("task cancelled", "id", req.Id)
	return &xagentv1.CancelTaskResponse{}, nil
}

func (s *Server) RestartTask(ctx context.Context, req *xagentv1.RestartTaskRequest) (*xagentv1.RestartTaskResponse, error) {
	err := s.tasks.WithTx(ctx, nil, func(tx *sql.Tx) error {
		task, err := s.tasks.Get(ctx, tx, req.Id)
		if err != nil {
			return err
		}
		if !task.Restart() {
			return fmt.Errorf("cannot restart task with status %s", task.Status)
		}
		if err := s.tasks.Put(ctx, tx, task); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.log.Info("task restarted", "id", req.Id)
	return &xagentv1.RestartTaskResponse{}, nil
}

func (s *Server) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	for _, entry := range req.Entries {
		log := model.LogFromProto(entry)
		log.TaskID = req.TaskId
		if err := s.logs.Create(ctx, nil, &log); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	return &xagentv1.UploadLogsResponse{}, nil
}

func (s *Server) ListLogs(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
	logs, err := s.logs.ListByTask(ctx, nil, req.TaskId)
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
	link := &model.Link{
		TaskID:    req.TaskId,
		Relevance: req.Relevance,
		URL:       req.Url,
		Title:     req.Title,
		Notify:    req.Notify,
		CreatedAt: time.Now(),
	}
	if err := s.links.Create(ctx, nil, link); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("link created", "task", req.TaskId, "relevance", req.Relevance, "url", req.Url)
	return &xagentv1.CreateLinkResponse{
		Link: link.Proto(),
	}, nil
}

func (s *Server) ListLinks(ctx context.Context, req *xagentv1.ListLinksRequest) (*xagentv1.ListLinksResponse, error) {
	links, err := s.links.ListByTask(ctx, nil, req.TaskId)
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
	links, err := s.links.FindByURL(ctx, nil, req.Url)
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

const maxLimit = 100

func (s *Server) ListEvents(ctx context.Context, req *xagentv1.ListEventsRequest) (*xagentv1.ListEventsResponse, error) {
	limit := cmp.Or(int(req.Limit), maxLimit)
	if limit < 0 || limit > maxLimit {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("limit must be at most %d", maxLimit))
	}
	events, err := s.events.List(ctx, nil, limit)
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
	event := &model.Event{
		Description: req.Description,
		Data:        req.Data,
		URL:         req.Url,
	}
	if err := s.events.Create(ctx, nil, event); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event created", "id", event.ID, "description", event.Description)
	return &xagentv1.CreateEventResponse{
		Event: event.Proto(),
	}, nil
}

func (s *Server) GetEvent(ctx context.Context, req *xagentv1.GetEventRequest) (*xagentv1.GetEventResponse, error) {
	event, err := s.events.Get(ctx, nil, req.Id)
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
	if err := s.events.Delete(ctx, nil, req.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event deleted", "id", req.Id)
	return &xagentv1.DeleteEventResponse{}, nil
}

func (s *Server) AddEventTask(ctx context.Context, req *xagentv1.AddEventTaskRequest) (*xagentv1.AddEventTaskResponse, error) {
	if err := s.events.AddTask(ctx, nil, req.EventId, req.TaskId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event task added", "event_id", req.EventId, "task_id", req.TaskId)
	return &xagentv1.AddEventTaskResponse{}, nil
}

func (s *Server) RemoveEventTask(ctx context.Context, req *xagentv1.RemoveEventTaskRequest) (*xagentv1.RemoveEventTaskResponse, error) {
	if err := s.events.RemoveTask(ctx, nil, req.EventId, req.TaskId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event task removed", "event_id", req.EventId, "task_id", req.TaskId)
	return &xagentv1.RemoveEventTaskResponse{}, nil
}

func (s *Server) ListEventTasks(ctx context.Context, req *xagentv1.ListEventTasksRequest) (*xagentv1.ListEventTasksResponse, error) {
	tasks, err := s.events.ListTasks(ctx, nil, req.EventId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListEventTasksResponse{TaskIds: tasks}, nil
}

func (s *Server) ListEventsByTask(ctx context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
	events, err := s.events.ListByTask(ctx, nil, req.TaskId)
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
	event, err := s.events.Get(ctx, nil, req.Id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("event %d not found", req.Id))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if event.URL == "" {
		return &xagentv1.ProcessEventResponse{}, nil
	}

	links, err := s.links.FindByURL(ctx, nil, event.URL)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Build set of tasks that want notifications
	taskIDs := map[int64]bool{}
	for _, link := range links {
		if !link.Notify || taskIDs[link.TaskID] {
			continue
		}
		// Skip archived tasks
		task, err := s.tasks.Get(ctx, nil, link.TaskID)
		if err != nil {
			s.log.Warn("failed to get task", "task_id", link.TaskID, "error", err)
			continue
		}
		if task.Status == model.TaskStatusArchived {
			s.log.Info("skipping archived task", "task_id", link.TaskID)
			continue
		}
		taskIDs[link.TaskID] = true
		if err := s.events.AddTask(ctx, nil, req.Id, link.TaskID); err != nil {
			s.log.Warn("failed to add event task", "event_id", req.Id, "task_id", link.TaskID, "error", err)
		}
		err = s.tasks.WithTx(ctx, nil, func(tx *sql.Tx) error {
			task, err := s.tasks.Get(ctx, tx, link.TaskID)
			if err != nil {
				return err
			}
			task.Start()
			if err := s.tasks.Put(ctx, tx, task); err != nil {
				return err
			}
			return tx.Commit()
		})
		if err != nil {
			s.log.Warn("failed to start task", "task_id", link.TaskID, "error", err)
		}
	}

	ids := slices.Collect(maps.Keys(taskIDs))
	s.log.Info("event processed", "id", req.Id, "tasks_routed", len(ids))
	return &xagentv1.ProcessEventResponse{TaskIds: ids}, nil
}

func (s *Server) SubmitRunnerEvents(ctx context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
	for _, pbEvent := range req.Events {
		event := model.RunnerEventFromProto(pbEvent)
		var task *model.Task
		var applied bool
		err := s.tasks.WithTx(ctx, nil, func(tx *sql.Tx) error {
			var err error
			task, err = s.tasks.Get(ctx, tx, event.TaskID)
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
			if err := s.tasks.Put(ctx, tx, task); err != nil {
				return err
			}
			if log, ok := s.toRunnerEventLog(event); ok {
				if err := s.logs.Create(ctx, tx, &log); err != nil {
					return err
				}
			}
			return tx.Commit()
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if s.notify && applied && task.Command == "" {
			s.sendNotification(task, event.Event)
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

func (s *Server) sendNotification(task *model.Task, event model.RunnerEventType) {
	displayName := fmt.Sprintf("Task %d", task.ID)
	if task.Name != "" {
		displayName = fmt.Sprintf("%q", task.Name)
	}

	var message string
	switch event {
	case model.RunnerEventStopped:
		message = fmt.Sprintf("%s completed", displayName)
	case model.RunnerEventFailed:
		message = fmt.Sprintf("%s failed", displayName)
	default:
		return
	}

	if err := notify.Send("xagent", message); err != nil {
		s.log.Error("failed to send notification", "task", task.ID, "error", err)
	}
}

func (s *Server) RegisterWorkspaces(ctx context.Context, req *xagentv1.RegisterWorkspacesRequest) (*xagentv1.RegisterWorkspacesResponse, error) {
	err := s.workspaces.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.workspaces.DeleteByRunner(ctx, tx, req.RunnerId); err != nil {
			return err
		}
		for _, ws := range req.Workspaces {
			if err := s.workspaces.Create(ctx, tx, req.RunnerId, ws.Name); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.log.Info("workspaces registered", "runner_id", req.RunnerId, "count", len(req.Workspaces))
	return &xagentv1.RegisterWorkspacesResponse{}, nil
}

func (s *Server) ListWorkspaces(ctx context.Context, req *xagentv1.ListWorkspacesRequest) (*xagentv1.ListWorkspacesResponse, error) {
	names, err := s.workspaces.List(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	workspaces := make([]*xagentv1.RegisteredWorkspace, len(names))
	for i, name := range names {
		workspaces[i] = &xagentv1.RegisteredWorkspace{Name: name}
	}

	return &xagentv1.ListWorkspacesResponse{Workspaces: workspaces}, nil
}
