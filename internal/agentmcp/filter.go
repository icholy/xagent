package agentmcp

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/agentauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
)

// AgentFilter implements XAgentServiceHandler and enforces task-scoped access
// control via the authscope engine. Each RPC names the operation and attributes
// it needs and checks them against the caller's scopes; those scopes are minted
// by the runner (see agentauth.Scopes) and only ever grant access to the
// agent's own task or, when the workspace enables it, its direct children.
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

// scopes returns the caller's scopes, or PermissionDenied if no claims are
// present. The scopes are already parsed during JWT verification (see
// authscope.Scopes), so this just unwraps them from the claims.
func (p *AgentFilter) scopes(ctx context.Context) (authscope.Scopes, error) {
	claims, ok := agentauth.ClaimsFromContext(ctx)
	if !ok {
		return nil, errPermissionDenied("missing agent token")
	}
	return claims.Scopes, nil
}

func (p *AgentFilter) Ping(ctx context.Context, req *xagentv1.PingRequest) (*xagentv1.PingResponse, error) {
	return &xagentv1.PingResponse{}, nil
}

func (p *AgentFilter) CreateLink(ctx context.Context, req *xagentv1.CreateLinkRequest) (*xagentv1.CreateLinkResponse, error) {
	scopes, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	allowed := scopes.Allow(authscope.OpTaskWrite, authscope.WithTaskID(req.TaskId))
	if !allowed {
		return nil, errPermissionDenied("can only create links for own task")
	}
	return p.client.CreateLink(ctx, req)
}

func (p *AgentFilter) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	scopes, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	allowed := scopes.Allow(authscope.OpTaskWrite, authscope.WithTaskID(req.TaskId))
	if !allowed {
		return nil, errPermissionDenied("can only upload logs for own task")
	}
	return p.client.UploadLogs(ctx, req)
}

func (p *AgentFilter) SubmitRunnerEvents(ctx context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
	scopes, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	// All-or-nothing: every event must authorize before any is forwarded.
	for _, ev := range req.Events {
		allowed := scopes.Allow(authscope.OpTaskWrite, authscope.WithTaskID(ev.TaskId))
		if !allowed {
			return nil, errPermissionDenied("can only submit events for own task")
		}
	}
	return p.client.SubmitRunnerEvents(ctx, req)
}

func (p *AgentFilter) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	scopes, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	allowed := scopes.Allow(authscope.OpTaskCreate,
		authscope.WithTaskParent(req.Parent),
		authscope.WithTaskWorkspace(req.Workspace),
		authscope.WithTaskRunner(req.Runner),
	)
	if !allowed {
		return nil, errPermissionDenied("can only create child tasks of own task")
	}
	return p.client.CreateTask(ctx, req)
}

func (p *AgentFilter) ListChildTasks(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
	scopes, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	allowed := scopes.Allow(authscope.OpTaskRead, authscope.WithTaskParent(req.ParentId))
	if !allowed {
		return nil, errPermissionDenied("can only list children of own task")
	}
	return p.client.ListChildTasks(ctx, req)
}

func (p *AgentFilter) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	scopes, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.GetTask(ctx, req)
	if err != nil {
		return nil, err
	}
	allowed := scopes.Allow(authscope.OpTaskRead,
		authscope.WithTaskID(resp.Task.Id),
		authscope.WithTaskParent(resp.Task.Parent),
	)
	if !allowed {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	return resp, nil
}

func (p *AgentFilter) GetTaskDetails(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
	scopes, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	details, err := p.client.GetTaskDetails(ctx, req)
	if err != nil {
		return nil, err
	}
	allowed := scopes.Allow(authscope.OpTaskRead,
		authscope.WithTaskID(details.Task.Id),
		authscope.WithTaskParent(details.Task.Parent),
	)
	if !allowed {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	return details, nil
}

func (p *AgentFilter) UpdateTask(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
	scopes, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: req.Id})
	if err != nil {
		return nil, err
	}
	allowed := scopes.Allow(authscope.OpTaskWrite,
		authscope.WithTaskID(resp.Task.Id),
		authscope.WithTaskParent(resp.Task.Parent),
	)
	if !allowed {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	// State guard: scope authorizes the write in principle, but an archived task
	// is not in a writable state.
	if resp.Task.Archived {
		return nil, errPermissionDenied("cannot update archived task")
	}
	return p.client.UpdateTask(ctx, req)
}

func (p *AgentFilter) ListLogs(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
	scopes, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	// Own-task fast path: authorize by id alone (matches task.read:{id:self})
	// so reading own logs needs no row load. Only the child case has to resolve
	// the row's parent.
	allowed := scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(req.TaskId))
	if !allowed {
		resp, err := p.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: req.TaskId})
		if err != nil {
			return nil, err
		}
		allowed := scopes.Allow(authscope.OpTaskRead,
			authscope.WithTaskID(resp.Task.Id),
			authscope.WithTaskParent(resp.Task.Parent),
		)
		if !allowed {
			return nil, errPermissionDenied("task is not a child of the current task")
		}
	}
	return p.client.ListLogs(ctx, req)
}

func (p *AgentFilter) CreateGitHubToken(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
	scopes, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	allowed := scopes.Allow(authscope.OpGitHubTokenCreate)
	if !allowed {
		return nil, errPermissionDenied("github token issuance is disabled for this workspace")
	}
	return p.client.CreateGitHubToken(ctx, req)
}

func errPermissionDenied(msg string) error {
	return connect.NewError(connect.CodePermissionDenied, errors.New(msg))
}
