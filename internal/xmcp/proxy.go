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

var ErrAccessDenied = connect.NewError(connect.CodePermissionDenied, errors.New("access denied"))

// isSelfOrChild checks if the given task ID is either this task or a direct child.
func (p *TaskProxy) isSelfOrChild(ctx context.Context, taskID int64) (bool, error) {
	if taskID == p.taskID {
		return true, nil
	}
	resp, err := p.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
	if err != nil {
		return false, err
	}
	return resp.Task.Parent == p.taskID, nil
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
		return nil, ErrAccessDenied
	}
	return p.client.CreateLink(ctx, req)
}

func (p *TaskProxy) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	if req.TaskId != p.taskID {
		return nil, ErrAccessDenied
	}
	return p.client.UploadLogs(ctx, req)
}

func (p *TaskProxy) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	if req.Parent != p.taskID {
		return nil, ErrAccessDenied
	}
	return p.client.CreateTask(ctx, req)
}

func (p *TaskProxy) ListChildTasks(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
	if req.ParentId != p.taskID {
		return nil, ErrAccessDenied
	}
	return p.client.ListChildTasks(ctx, req)
}

// Self or direct child: fetch-then-validate
func (p *TaskProxy) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	resp, err := p.client.GetTask(ctx, req)
	if err != nil {
		return nil, err
	}
	// Allow if self or direct child
	if resp.Task.Id == p.taskID || resp.Task.Parent == p.taskID {
		return resp, nil
	}
	return nil, ErrAccessDenied
}

func (p *TaskProxy) GetTaskDetails(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
	// Check if self
	if req.Id == p.taskID {
		return p.client.GetTaskDetails(ctx, req)
	}
	// Check if direct child
	ok, err := p.isChild(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrAccessDenied
	}
	return p.client.GetTaskDetails(ctx, req)
}

func (p *TaskProxy) UpdateTask(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
	// Check if self or direct child
	ok, err := p.isSelfOrChild(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrAccessDenied
	}
	return p.client.UpdateTask(ctx, req)
}

func (p *TaskProxy) ListLogs(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
	// Only allow for direct children (not self - use get_my_task for that)
	ok, err := p.isChild(ctx, req.TaskId)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrAccessDenied
	}
	return p.client.ListLogs(ctx, req)
}
