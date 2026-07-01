package apiserver

import (
	"context"
	"database/sql"
	"errors"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

func (s *Server) RegisterWorkspaces(ctx context.Context, req *xagentv1.RegisterWorkspacesRequest) (*xagentv1.RegisterWorkspacesResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpWorkspaceWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot register workspaces"))
	}
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.store.DeleteWorkspacesByRunner(ctx, tx, req.RunnerId, caller.OrgID); err != nil {
			return err
		}
		for _, ws := range req.Workspaces {
			if err := s.store.CreateWorkspace(ctx, tx, req.RunnerId, ws.Name, ws.Description, caller.OrgID); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("workspaces registered", "runner_id", req.RunnerId, "org_id", caller.OrgID, "count", len(req.Workspaces))
	return &xagentv1.RegisterWorkspacesResponse{}, nil
}

func (s *Server) ListWorkspaces(ctx context.Context, req *xagentv1.ListWorkspacesRequest) (*xagentv1.ListWorkspacesResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpWorkspaceRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot list workspaces"))
	}
	workspaces, err := s.store.ListWorkspaces(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListWorkspacesResponse{Workspaces: model.ProtoMap(workspaces)}, nil
}

func (s *Server) ClearWorkspaces(ctx context.Context, req *xagentv1.ClearWorkspacesRequest) (*xagentv1.ClearWorkspacesResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpWorkspaceWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot clear workspaces"))
	}
	if req.RunnerId != "" {
		if err := s.store.DeleteWorkspacesByRunner(ctx, nil, req.RunnerId, caller.OrgID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		s.log.Info("workspaces cleared", "org_id", caller.OrgID, "runner", req.RunnerId)
	} else {
		if err := s.store.ClearWorkspaces(ctx, nil, caller.OrgID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		s.log.Info("workspaces cleared", "org_id", caller.OrgID)
	}
	return &xagentv1.ClearWorkspacesResponse{}, nil
}
