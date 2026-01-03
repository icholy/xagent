package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/store"
)

type Server struct {
	xagentv1connect.UnimplementedXAgentServiceHandler
	log   *slog.Logger
	tasks *store.TaskRepository
	logs  *store.LogRepository
	links *store.LinkRepository
}

type Options struct {
	Log   *slog.Logger
	Tasks *store.TaskRepository
	Logs  *store.LogRepository
	Links *store.LinkRepository
}

func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		log:   log,
		tasks: opts.Tasks,
		logs:  opts.Logs,
		links: opts.Links,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// API
	path, handler := xagentv1connect.NewXAgentServiceHandler(s)
	mux.Handle(path, handler)

	// UI
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /tasks", s.handleTaskList)
	mux.HandleFunc("GET /tasks/{id}", s.handleTaskDetail)
	mux.HandleFunc("GET /tasks/{id}/detail", s.handleTaskDetailPartial)
	mux.HandleFunc("GET /tasks/{id}/logs", s.handleTaskLogs)

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

func (s *Server) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	id := req.Id
	if id == "" {
		id = uuid.NewString()
	}

	instructions := make([]store.Instruction, len(req.Instructions))
	for i, inst := range req.Instructions {
		instructions[i] = store.Instruction{
			Text: inst.Text,
			URL:  inst.Url,
		}
	}

	task := &store.Task{
		ID:           id,
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

func (s *Server) UpdateTask(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
	instructions := make([]store.Instruction, len(req.AddInstructions))
	for i, inst := range req.AddInstructions {
		instructions[i] = store.Instruction{
			Text: inst.Text,
			URL:  inst.Url,
		}
	}

	if err := s.tasks.Update(req.Id, store.TaskUpdate{
		Status:          store.TaskStatus(req.Status),
		AddInstructions: instructions,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.log.Info("task updated", "id", req.Id, "status", req.Status, "instructions_added", len(req.AddInstructions))
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
		Workspace:    t.Workspace,
		Instructions: instructions,
		Status:       string(t.Status),
		CreatedAt:    t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func linkToProto(l *store.Link) *xagentv1.TaskLink {
	return &xagentv1.TaskLink{
		Id:        l.ID,
		TaskId:    l.TaskID,
		Relevance: l.Relevance,
		Url:       l.URL,
		Title:     l.Title,
		CreatedAt: l.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Created:   l.Created,
	}
}
