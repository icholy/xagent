package apiserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/agentauth"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

// CreateTaskToken mints a narrow app JWT for a task. The runner calls it instead
// of signing a token itself: it supplies only the task id and the workspace
// capability flags, and the server derives the task's workspace/runner/org from
// the authoritative task row (never the request) before minting the scopes. The
// minted token is an ordinary apiauth.AppClaims signed with the server's app key,
// so it verifies on the normal app-JWT path; its authority lives entirely in its
// narrow scopes. See proposals/draft/eliminate-runner-socket-proxy.md §1/§2/§7.
func (s *Server) CreateTaskToken(ctx context.Context, req *xagentv1.CreateTaskTokenRequest) (*xagentv1.CreateTaskTokenResponse, error) {
	caller := apiauth.MustCaller(ctx)
	// Capability-presence check: minting a task token has no instance attribute, so
	// gate on AllowOp (proposal §5/§7), not the instance-aware Allow.
	if !caller.Scopes.AllowOp(authscope.OpTaskTokenCreate) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot mint task tokens"))
	}
	// Tenancy: the task must belong to the caller's org. An org-scoped read also
	// turns another org's task into NotFound rather than leaking its existence.
	task, err := s.store.GetTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	// The runner cannot widen the sandbox: workspace and runner come from the row,
	// only the capability flags come from the request.
	scopes := agentauth.Scopes(agentauth.ScopeOptions{
		TaskID:       task.ID,
		Workspace:    task.Workspace,
		Runner:       task.Runner,
		Capabilities: validCapabilities(req.Capabilities),
	})
	token, err := apiauth.SignAppToken(s.appKey, apiauth.NewTaskTokenClaims(task.OrgID, scopes))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("task token minted", "task_id", task.ID, "org_id", task.OrgID)
	return &xagentv1.CreateTaskTokenResponse{Token: token}, nil
}

// validCapabilities returns the subset of caps that are recognized capability
// flags. An unrecognized flag can only ever grant nothing (agentauth.Scopes
// matches by exact name), so dropping it keeps a runner from smuggling in an
// unknown grant while staying forward-compatible with newer runners.
func validCapabilities(caps []string) []string {
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		if agentauth.ValidCapability(c) {
			out = append(out, c)
		}
	}
	return out
}
