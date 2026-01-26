package xmcp

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
)

// AgentFilter implements XAgentServiceHandler and enforces task-scoped access control.
// It only allows operations on the agent's own task or its direct children.
type AgentFilter struct {
	xagentv1connect.UnimplementedXAgentServiceHandler
	task   *model.Task
	client xagentv1connect.XAgentServiceClient
}

// NewAgentFilter creates a new task-scoped filter.
func NewAgentFilter(task *model.Task, client xagentv1connect.XAgentServiceClient) *AgentFilter {
	return &AgentFilter{
		task:   task,
		client: client,
	}
}

func (p *AgentFilter) CreateLink(ctx context.Context, req *xagentv1.CreateLinkRequest) (*xagentv1.CreateLinkResponse, error) {
	if req.TaskId != p.task.ID {
		return nil, errPermissionDenied("can only create links for own task")
	}
	return p.client.CreateLink(ctx, req)
}

func (p *AgentFilter) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	if req.TaskId != p.task.ID {
		return nil, errPermissionDenied("can only upload logs for own task")
	}
	return p.client.UploadLogs(ctx, req)
}

func (p *AgentFilter) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	if req.Parent != p.task.ID {
		return nil, errPermissionDenied("can only create child tasks of own task")
	}
	if req.Workspace != p.task.Workspace {
		return nil, errPermissionDenied("can only create tasks in same workspace")
	}
	if req.Runner != p.task.Runner {
		return nil, errPermissionDenied("can only create tasks in same runner")
	}
	return p.client.CreateTask(ctx, req)
}

func (p *AgentFilter) ListChildTasks(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
	if req.ParentId != p.task.ID {
		return nil, errPermissionDenied("can only list children of own task")
	}
	return p.client.ListChildTasks(ctx, req)
}

func (p *AgentFilter) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	resp, err := p.client.GetTask(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Task.Id == p.task.ID || resp.Task.Parent == p.task.ID {
		return resp, nil
	}
	return nil, errPermissionDenied("task is not a child of the current task")
}

func (p *AgentFilter) GetTaskDetails(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
	if req.Id == p.task.ID {
		return p.client.GetTaskDetails(ctx, req)
	}
	details, err := p.client.GetTaskDetails(ctx, req)
	if err != nil {
		return nil, err
	}
	if details.Task.Parent != p.task.ID {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	return details, nil
}

func (p *AgentFilter) UpdateTask(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
	resp, err := p.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: req.Id})
	if err != nil {
		return nil, err
	}
	if resp.Task.Id != p.task.ID && resp.Task.Parent != p.task.ID {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	if resp.Task.Status == "archived" {
		return nil, errPermissionDenied("cannot update archived task")
	}
	return p.client.UpdateTask(ctx, req)
}

func (p *AgentFilter) ListLogs(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
	if req.TaskId != p.task.ID {
		resp, err := p.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: req.TaskId})
		if err != nil {
			return nil, err
		}
		if resp.Task.Parent != p.task.ID {
			return nil, errPermissionDenied("task is not a child of the current task")
		}
	}
	return p.client.ListLogs(ctx, req)
}

func errPermissionDenied(msg string) error {
	return connect.NewError(connect.CodePermissionDenied, errors.New(msg))
}
