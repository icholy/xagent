package xmcp

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
)

// TaskProxy implements XAgentServiceHandler and enforces task-scoped access control.
// It only allows operations on the proxy's own task or its direct children.
type TaskProxy struct {
	xagentv1connect.UnimplementedXAgentServiceHandler
	taskID int64
	client xagentv1connect.XAgentServiceClient
}

// NewTaskProxy creates a new task-scoped proxy.
func NewTaskProxy(taskID int64, client xagentv1connect.XAgentServiceClient) *TaskProxy {
	return &TaskProxy{
		taskID: taskID,
		client: client,
	}
}

func errPermissionDenied(msg string) error {
	return connect.NewError(connect.CodePermissionDenied, errors.New(msg))
}

// isChild checks if the given task ID is a direct child (not self).
func (p *TaskProxy) isChild(ctx context.Context, taskID int64) (bool, error) {
	if taskID == p.taskID {
		return false, nil
	}
	resp, err := p.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
	if err != nil {
		return false, err
	}
	return resp.Task.Parent == p.taskID, nil
}

// Self-only operations: validate task ID matches

func (p *TaskProxy) CreateLink(ctx context.Context, req *xagentv1.CreateLinkRequest) (*xagentv1.CreateLinkResponse, error) {
	if req.TaskId != p.taskID {
		return nil, errPermissionDenied("can only create links for own task")
	}
	return p.client.CreateLink(ctx, req)
}

func (p *TaskProxy) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	if req.TaskId != p.taskID {
		return nil, errPermissionDenied("can only upload logs for own task")
	}
	return p.client.UploadLogs(ctx, req)
}

func (p *TaskProxy) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	if req.Parent != p.taskID {
		return nil, errPermissionDenied("can only create child tasks of own task")
	}
	return p.client.CreateTask(ctx, req)
}

func (p *TaskProxy) ListChildTasks(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
	if req.ParentId != p.taskID {
		return nil, errPermissionDenied("can only list children of own task")
	}
	return p.client.ListChildTasks(ctx, req)
}

func (p *TaskProxy) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	resp, err := p.client.GetTask(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Task.Id == p.taskID || resp.Task.Parent == p.taskID {
		return resp, nil
	}
	return nil, errPermissionDenied("task is not a child of the current task")
}

func (p *TaskProxy) GetTaskDetails(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
	if req.Id == p.taskID {
		return p.client.GetTaskDetails(ctx, req)
	}
	ok, err := p.isChild(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	return p.client.GetTaskDetails(ctx, req)
}

func (p *TaskProxy) UpdateTask(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
	resp, err := p.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: req.Id})
	if err != nil {
		return nil, err
	}
	if resp.Task.Id != p.taskID && resp.Task.Parent != p.taskID {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	if resp.Task.Status == "archived" {
		return nil, errPermissionDenied("cannot update archived task")
	}
	return p.client.UpdateTask(ctx, req)
}

func (p *TaskProxy) ListLogs(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
	ok, err := p.isChild(ctx, req.TaskId)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	return p.client.ListLogs(ctx, req)
}
