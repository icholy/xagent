package agentmcp

import (
	"context"
	"errors"
	"strconv"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/agentauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
)

// AgentFilter implements XAgentServiceHandler and enforces task-scoped access
// control via the authscope engine. Each RPC builds a Target and authorizes it
// against the caller's scope set; the set is minted by the runner (see
// agentauth.TaskScopes) and only ever grants access to the agent's own task or,
// when the workspace enables it, its direct children. Claims must be present in
// context (injected by agentauth.Middleware).
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

// scopes returns the caller's parsed scope set, or PermissionDenied if no claims
// are present or the minted scopes are malformed.
func (p *AgentFilter) scopes(ctx context.Context) (authscope.Set, error) {
	claims, ok := agentauth.ClaimsFromContext(ctx)
	if !ok {
		return nil, errPermissionDenied("missing agent token")
	}
	set, err := authscope.ParseSet(claims.Scopes)
	if err != nil {
		return nil, errPermissionDenied("invalid token scopes")
	}
	return set, nil
}

// taskTarget builds a task-resource Target for the given action and attributes.
func taskTarget(action string, attrs map[string]string) authscope.Target {
	return authscope.Target{
		Op:    []string{agentauth.SegTask, action},
		Attrs: attrs,
	}
}

func idStr(id int64) string {
	return strconv.FormatInt(id, 10)
}

func (p *AgentFilter) Ping(ctx context.Context, req *xagentv1.PingRequest) (*xagentv1.PingResponse, error) {
	return &xagentv1.PingResponse{}, nil
}

func (p *AgentFilter) CreateLink(ctx context.Context, req *xagentv1.CreateLinkRequest) (*xagentv1.CreateLinkResponse, error) {
	set, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	target := taskTarget(agentauth.SegWrite, map[string]string{agentauth.AttrID: idStr(req.TaskId)})
	if !set.Authorize(target) {
		return nil, errPermissionDenied("can only create links for own task")
	}
	return p.client.CreateLink(ctx, req)
}

func (p *AgentFilter) UploadLogs(ctx context.Context, req *xagentv1.UploadLogsRequest) (*xagentv1.UploadLogsResponse, error) {
	set, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	target := taskTarget(agentauth.SegWrite, map[string]string{agentauth.AttrID: idStr(req.TaskId)})
	if !set.Authorize(target) {
		return nil, errPermissionDenied("can only upload logs for own task")
	}
	return p.client.UploadLogs(ctx, req)
}

func (p *AgentFilter) SubmitRunnerEvents(ctx context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
	set, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	// All-or-nothing: every event must authorize before any is forwarded.
	for _, ev := range req.Events {
		target := taskTarget(agentauth.SegWrite, map[string]string{agentauth.AttrID: idStr(ev.TaskId)})
		if !set.Authorize(target) {
			return nil, errPermissionDenied("can only submit events for own task")
		}
	}
	return p.client.SubmitRunnerEvents(ctx, req)
}

func (p *AgentFilter) CreateTask(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
	set, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	target := taskTarget(agentauth.SegCreate, map[string]string{
		agentauth.AttrParent:    idStr(req.Parent),
		agentauth.AttrWorkspace: req.Workspace,
		agentauth.AttrRunner:    req.Runner,
	})
	if !set.Authorize(target) {
		return nil, errPermissionDenied("can only create child tasks of own task")
	}
	return p.client.CreateTask(ctx, req)
}

func (p *AgentFilter) ListChildTasks(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
	set, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	target := taskTarget(agentauth.SegRead, map[string]string{agentauth.AttrParent: idStr(req.ParentId)})
	if !set.Authorize(target) {
		return nil, errPermissionDenied("can only list children of own task")
	}
	return p.client.ListChildTasks(ctx, req)
}

func (p *AgentFilter) GetTask(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
	set, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.GetTask(ctx, req)
	if err != nil {
		return nil, err
	}
	target := taskTarget(agentauth.SegRead, map[string]string{
		agentauth.AttrID:     idStr(resp.Task.Id),
		agentauth.AttrParent: idStr(resp.Task.Parent),
	})
	if !set.Authorize(target) {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	return resp, nil
}

func (p *AgentFilter) GetTaskDetails(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
	set, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	details, err := p.client.GetTaskDetails(ctx, req)
	if err != nil {
		return nil, err
	}
	target := taskTarget(agentauth.SegRead, map[string]string{
		agentauth.AttrID:     idStr(details.Task.Id),
		agentauth.AttrParent: idStr(details.Task.Parent),
	})
	if !set.Authorize(target) {
		return nil, errPermissionDenied("task is not a child of the current task")
	}
	return details, nil
}

func (p *AgentFilter) UpdateTask(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
	set, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: req.Id})
	if err != nil {
		return nil, err
	}
	target := taskTarget(agentauth.SegWrite, map[string]string{
		agentauth.AttrID:     idStr(resp.Task.Id),
		agentauth.AttrParent: idStr(resp.Task.Parent),
	})
	if !set.Authorize(target) {
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
	set, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	// Own-task fast path: authorize by id alone (matches task.read:{id:self})
	// so reading own logs needs no row load. Only the child case has to resolve
	// the row's parent.
	if !set.Authorize(taskTarget(agentauth.SegRead, map[string]string{agentauth.AttrID: idStr(req.TaskId)})) {
		resp, err := p.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: req.TaskId})
		if err != nil {
			return nil, err
		}
		target := taskTarget(agentauth.SegRead, map[string]string{
			agentauth.AttrID:     idStr(resp.Task.Id),
			agentauth.AttrParent: idStr(resp.Task.Parent),
		})
		if !set.Authorize(target) {
			return nil, errPermissionDenied("task is not a child of the current task")
		}
	}
	return p.client.ListLogs(ctx, req)
}

func (p *AgentFilter) CreateGitHubToken(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
	set, err := p.scopes(ctx)
	if err != nil {
		return nil, err
	}
	target := authscope.Target{Op: []string{agentauth.SegGitHubToken, agentauth.SegCreate}}
	if !set.Authorize(target) {
		return nil, errPermissionDenied("github token issuance is disabled for this workspace")
	}
	return p.client.CreateGitHubToken(ctx, req)
}

func errPermissionDenied(msg string) error {
	return connect.NewError(connect.CodePermissionDenied, errors.New(msg))
}
