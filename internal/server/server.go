package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	xagentv1connect.UnimplementedXAgentServiceHandler
	log    *slog.Logger
	tasks  *store.TaskRepository
	logs   *store.LogRepository
	links  *store.LinkRepository
	events *store.EventRepository
}

type Options struct {
	Log    *slog.Logger
	Tasks  *store.TaskRepository
	Logs   *store.LogRepository
	Links  *store.LinkRepository
	Events *store.EventRepository
}

func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		log:    log,
		tasks:  opts.Tasks,
		logs:   opts.Logs,
		links:  opts.Links,
		events: opts.Events,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// API
	path, handler := xagentv1connect.NewXAgentServiceHandler(s)
	mux.Handle(path, handler)

	// UI
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/tasks", http.StatusFound)
	})
	mux.HandleFunc("GET /ui/tasks", s.handleIndex)
	mux.HandleFunc("GET /ui/tasks/list", s.handleTaskList)
	mux.HandleFunc("GET /ui/tasks/{id}", s.handleTaskDetail)
	mux.HandleFunc("GET /ui/tasks/{id}/detail", s.handleTaskDetailPartial)
	mux.HandleFunc("GET /ui/tasks/{id}/logs", s.handleTaskLogs)
	mux.HandleFunc("GET /ui/tasks/{id}/children", s.handleTaskChildren)
	mux.HandleFunc("GET /ui/events", s.handleEvents)
	mux.HandleFunc("GET /ui/events/list", s.handleEventList)
	mux.HandleFunc("GET /ui/events/{id}", s.handleEventDetail)

	return mux
}

func (s *Server) ListTasks(ctx context.Context, req *xagentv1.ListTasksRequest) (*xagentv1.ListTasksResponse, error) {
	var tasks []*store.Task
	var err error
	if len(req.Statuses) > 0 {
		statuses := make([]store.TaskStatus, len(req.Statuses))
		for i, s := range req.Statuses {
			statuses[i] = store.TaskStatus(s)
		}
		tasks, err = s.tasks.ListByStatuses(statuses)
	} else {
		tasks, err = s.tasks.List()
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	resp := &xagentv1.ListTasksResponse{
		Tasks: make([]*xagentv1.Task, len(tasks)),
	}
	for i, t := range tasks {
		resp.Tasks[i] = taskToProto(t)
	}
	return resp, nil
}

func (s *Server) ListChildTasks(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
	tasks, err := s.tasks.ListChildren(req.ParentId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	resp := &xagentv1.ListChildTasksResponse{
		Tasks: make([]*xagentv1.Task, len(tasks)),
	}
	for i, t := range tasks {
		resp.Tasks[i] = taskToProto(t)
	}
	return resp, nil
}

func (s *Server) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	instructions := make([]store.Instruction, len(req.Instructions))
	for i, inst := range req.Instructions {
		instructions[i] = store.Instruction{
			Text: inst.Text,
			URL:  inst.Url,
		}
	}

	task := &store.Task{
		Name:         req.Name,
		Parent:       req.Parent,
		Workspace:    req.Workspace,
		Instructions: instructions,
		Status:       store.TaskStatusPending,
	}

	if err := s.tasks.Create(task); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.log.Info("task created", "id", task.ID, "workspace", task.Workspace)
	return &xagentv1.CreateTaskResponse{
		Task: taskToProto(task),
	}, nil
}

func (s *Server) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	task, err := s.tasks.Get(req.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	return &xagentv1.GetTaskResponse{
		Task: taskToProto(task),
	}, nil
}

func (s *Server) GetTaskDetails(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
	task, err := s.tasks.Get(req.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	children, _ := s.tasks.ListChildren(req.Id)
	events, _ := s.events.ListByTask(req.Id)
	links, _ := s.links.ListByTask(req.Id)

	resp := &xagentv1.GetTaskDetailsResponse{
		Task:     taskToProto(task),
		Children: make([]*xagentv1.Task, len(children)),
		Events:   make([]*xagentv1.Event, len(events)),
		Links:    make([]*xagentv1.TaskLink, len(links)),
	}
	for i, c := range children {
		resp.Children[i] = taskToProto(c)
	}
	for i, e := range events {
		resp.Events[i] = eventToProto(e)
	}
	for i, l := range links {
		resp.Links[i] = linkToProto(l)
	}
	return resp, nil
}

func (s *Server) UpdateTask(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
	instructions := make([]store.Instruction, len(req.AddInstructions))
	for i, inst := range req.AddInstructions {
		instructions[i] = store.Instruction{
			Text: inst.Text,
			URL:  inst.Url,
		}
	}

	if err := s.tasks.Update(req.Id, store.TaskUpdate{
		Name:            req.Name,
		Status:          store.TaskStatus(req.Status),
		AddInstructions: instructions,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.log.Info("task updated", "id", req.Id, "name", req.Name, "status", req.Status, "instructions_added", len(req.AddInstructions))
	return &xagentv1.UpdateTaskResponse{}, nil
}

func (s *Server) DeleteTask(ctx context.Context, req *xagentv1.DeleteTaskRequest) (*xagentv1.DeleteTaskResponse, error) {
	if err := s.tasks.Delete(req.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.log.Info("task deleted", "id", req.Id)
	return &xagentv1.DeleteTaskResponse{}, nil
}

func (s *Server) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	logs := make([]*store.Log, len(req.Entries))
	for i, entry := range req.Entries {
		logs[i] = &store.Log{
			TaskID:  req.TaskId,
			Type:    entry.Type,
			Content: entry.Content,
		}
	}
	if err := s.logs.CreateBatch(logs); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.UploadLogsResponse{}, nil
}

func (s *Server) ListLogs(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
	logs, err := s.logs.ListByTask(req.TaskId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListLogsResponse{
		Entries: make([]*xagentv1.LogEntry, len(logs)),
	}
	for i, l := range logs {
		resp.Entries[i] = &xagentv1.LogEntry{
			Type:    l.Type,
			Content: l.Content,
		}
	}
	return resp, nil
}

func (s *Server) CreateLink(ctx context.Context, req *xagentv1.CreateLinkRequest) (*xagentv1.CreateLinkResponse, error) {
	link := &store.Link{
		TaskID:    req.TaskId,
		Relevance: req.Relevance,
		URL:       req.Url,
		Title:     req.Title,
		CreatedAt: time.Now(),
		Created:   req.Created,
	}
	if err := s.links.Create(link); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("link created", "task", req.TaskId, "relevance", req.Relevance, "url", req.Url)
	return &xagentv1.CreateLinkResponse{
		Link: linkToProto(link),
	}, nil
}

func (s *Server) ListLinks(ctx context.Context, req *xagentv1.ListLinksRequest) (*xagentv1.ListLinksResponse, error) {
	links, err := s.links.ListByTask(req.TaskId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListLinksResponse{
		Links: make([]*xagentv1.TaskLink, len(links)),
	}
	for i, l := range links {
		resp.Links[i] = linkToProto(l)
	}
	return resp, nil
}

func (s *Server) FindLinksByURL(ctx context.Context, req *xagentv1.FindLinksByURLRequest) (*xagentv1.FindLinksByURLResponse, error) {
	links, err := s.links.FindByURL(req.Url)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.FindLinksByURLResponse{
		Links: make([]*xagentv1.TaskLink, len(links)),
	}
	for i, l := range links {
		resp.Links[i] = linkToProto(l)
	}
	return resp, nil
}

func taskToProto(t *store.Task) *xagentv1.Task {
	instructions := make([]*xagentv1.Instruction, len(t.Instructions))
	for i, inst := range t.Instructions {
		instructions[i] = &xagentv1.Instruction{
			Text: inst.Text,
			Url:  inst.URL,
		}
	}
	return &xagentv1.Task{
		Id:           t.ID,
		Name:         t.Name,
		Parent:       t.Parent,
		Workspace:    t.Workspace,
		Instructions: instructions,
		Status:       string(t.Status),
		CreatedAt:    timestamppb.New(t.CreatedAt),
		UpdatedAt:    timestamppb.New(t.UpdatedAt),
	}
}

func linkToProto(l *store.Link) *xagentv1.TaskLink {
	return &xagentv1.TaskLink{
		Id:        l.ID,
		TaskId:    l.TaskID,
		Relevance: l.Relevance,
		Url:       l.URL,
		Title:     l.Title,
		CreatedAt: timestamppb.New(l.CreatedAt),
		Created:   l.Created,
	}
}

func (s *Server) ListEvents(ctx context.Context, req *xagentv1.ListEventsRequest) (*xagentv1.ListEventsResponse, error) {
	events, err := s.events.List()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListEventsResponse{
		Events: make([]*xagentv1.Event, len(events)),
	}
	for i, e := range events {
		resp.Events[i] = eventToProto(e)
	}
	return resp, nil
}

func (s *Server) CreateEvent(ctx context.Context, req *xagentv1.CreateEventRequest) (*xagentv1.CreateEventResponse, error) {
	event := &store.Event{
		Description: req.Description,
		Data:        req.Data,
		URL:         req.Url,
	}
	if err := s.events.Create(event); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event created", "id", event.ID, "description", event.Description)
	return &xagentv1.CreateEventResponse{
		Event: eventToProto(event),
	}, nil
}

func (s *Server) GetEvent(ctx context.Context, req *xagentv1.GetEventRequest) (*xagentv1.GetEventResponse, error) {
	event, err := s.events.Get(req.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return &xagentv1.GetEventResponse{
		Event: eventToProto(event),
	}, nil
}

func (s *Server) DeleteEvent(ctx context.Context, req *xagentv1.DeleteEventRequest) (*xagentv1.DeleteEventResponse, error) {
	if err := s.events.Delete(req.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event deleted", "id", req.Id)
	return &xagentv1.DeleteEventResponse{}, nil
}

func (s *Server) AddEventTask(ctx context.Context, req *xagentv1.AddEventTaskRequest) (*xagentv1.AddEventTaskResponse, error) {
	if err := s.events.AddTask(req.EventId, req.TaskId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event task added", "event_id", req.EventId, "task_id", req.TaskId)
	return &xagentv1.AddEventTaskResponse{}, nil
}

func (s *Server) RemoveEventTask(ctx context.Context, req *xagentv1.RemoveEventTaskRequest) (*xagentv1.RemoveEventTaskResponse, error) {
	if err := s.events.RemoveTask(req.EventId, req.TaskId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("event task removed", "event_id", req.EventId, "task_id", req.TaskId)
	return &xagentv1.RemoveEventTaskResponse{}, nil
}

func (s *Server) ListEventTasks(ctx context.Context, req *xagentv1.ListEventTasksRequest) (*xagentv1.ListEventTasksResponse, error) {
	tasks, err := s.events.ListTasks(req.EventId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListEventTasksResponse{TaskIds: tasks}, nil
}

func (s *Server) ListEventsByTask(ctx context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
	events, err := s.events.ListByTask(req.TaskId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.ListEventsByTaskResponse{
		Events: make([]*xagentv1.Event, len(events)),
	}
	for i, e := range events {
		resp.Events[i] = eventToProto(e)
	}
	return resp, nil
}

func eventToProto(e *store.Event) *xagentv1.Event {
	return &xagentv1.Event{
		Id:          e.ID,
		Description: e.Description,
		Data:        e.Data,
		Url:         e.URL,
		CreatedAt:   timestamppb.New(e.CreatedAt),
	}
}
