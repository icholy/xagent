package agentmcp

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/agentauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
)

// AgentFilter implements XAgentServiceHandler and enforces task-scoped access control.
// It only allows operations on the agent's own task or its direct children.
// Claims must be present in context (injected by agentauth.Middleware).
type AgentFilter struct {
	xagentv1connect.UnimplementedXAgentServiceHandler
	client xagentv1connect.XAgentServiceClient
}

// NewAgentFilter creates a new task-scoped filter.
func NewAgentFilter(client xagentv1connect.XAgentServiceClient) *AgentFilter {
	return &AgentFilter{
		client: client,
	}
}

func (p *AgentFilter) claims(ctx context.Context) (*agentauth.TaskClaims, error) {
	claims, ok := agentauth.ClaimsFromContext(ctx)
	if !ok {
		return nil, errPermissionDenied("missing agent token")
	}
	return claims, nil
}

func (p *AgentFilter) Ping(ctx context.Context, req *xagentv1.PingRequest) (*xagentv1.PingResponse, error) {
	return &xagentv1.PingResponse{}, nil
}

func (p *AgentFilter) CreateLink(ctx context.Context, req *xagentv1.CreateLinkRequest) (*xagentv1.CreateLinkResponse, error) {
	claims, err := p.claims(ctx)
	if err != nil {
		return nil, err
	}
	if req.TaskId != claims.TaskID {
		return nil, errPermissionDenied("can only create links for own task")
	}
	return p.client.CreateLink(ctx, req)
}

func (p *AgentFilter) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	claims, err := p.claims(ctx)
	if err != nil {
		return nil, err
	}
	if req.TaskId != claims.TaskID {
		return nil, errPermissionDenied("can only upload logs for own task")
	}
	return p.client.UploadLogs(ctx, req)
}

func (p *AgentFilter) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	claims, err := p.claims(ctx)
	if err != nil {
		return nil, err
	}
	if req.Parent != claims.TaskID {
		return nil, errPermissionDenied("can only create child tasks of own task")
	}
	if req.Workspace != claims.Workspace {
		return nil, errPermissionDenied("can only create tasks in same workspace")
	}
	if req.Runner != claims.Runner {
		return nil, errPermissionDenied("can only create tasks in same runner")
	}
	return p.client.CreateTask(ctx, req)
}

func (p *AgentFilter) ListChildTasks(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
	claims, err := p.claims(ctx)
	if err != nil {
		return nil, err
	}
	if req.ParentId != claims.TaskID {
		return nil, errPermissionDenied("can only list children of own task")
	}
	return p.client.ListChildTasks(ctx, req)
}

func (p *AgentFilter) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	claims, err := p.claims(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.GetTask(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Task.Id == claims.TaskID || resp.Task.Parent == claims.TaskID {
		return resp, nil
	}
	return nil, errPermissionDenied("task is not a child of the current task")
}

func (p *AgentFilter) GetTaskDetails(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
	claims, err := p.claims(ctx)
	if err != nil {
		return nil, err
	}
	if req.Id == claims.TaskID {
		return p.client.GetTaskDetails(ctx, req)
	}
	details, err := p.client.GetTaskDetails(ctx, req)
	if err != nil {
		return nil, err
	}
	if details.Task.Parent != claims.TaskID {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	return details, nil
}

func (p *AgentFilter) UpdateTask(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
	claims, err := p.claims(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: req.Id})
	if err != nil {
		return nil, err
	}
	if resp.Task.Id != claims.TaskID && resp.Task.Parent != claims.TaskID {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	if resp.Task.Archived {
		return nil, errPermissionDenied("cannot update archived task")
	}
	return p.client.UpdateTask(ctx, req)
}

func (p *AgentFilter) ListLogs(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
	claims, err := p.claims(ctx)
	if err != nil {
		return nil, err
	}
	if req.TaskId != claims.TaskID {
		resp, err := p.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: req.TaskId})
		if err != nil {
			return nil, err
		}
		if resp.Task.Parent != claims.TaskID {
			return nil, errPermissionDenied("task is not a child of the current task")
		}
	}
	return p.client.ListLogs(ctx, req)
}

func errPermissionDenied(msg string) error {
	return connect.NewError(connect.CodePermissionDenied, errors.New(msg))
}
