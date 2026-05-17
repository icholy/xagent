package apiserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"connectrpc.com/connect"
	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) LinkGitHubInstallation(ctx context.Context, req *xagentv1.LinkGitHubInstallationRequest) (*xagentv1.LinkGitHubInstallationResponse, error) {
	caller := apiauth.Caller(ctx)
	if caller == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}
	if req.InstallationId == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("installation_id is required"))
	}

	externalID := strconv.FormatInt(req.InstallationId, 10)
	// Run the read and the write inside the same transaction so the pending
	// row can't be raced by a concurrent webhook between verify and promote.
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		user, err := s.store.GetUser(ctx, tx, caller.ID)
		if err != nil {
			return err
		}
		if !user.HasGitHub() {
			return connect.NewError(connect.CodeFailedPrecondition, errors.New("link your GitHub account first at /github/login"))
		}
		pending, err := s.store.GetPendingIntegration(ctx, tx, model.PendingIntegrationTypeGitHub, externalID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return connect.NewError(connect.CodeNotFound, fmt.Errorf("no pending GitHub installation with id %d", req.InstallationId))
			}
			return err
		}
		if pending.Options.GitHub == nil {
			return errors.New("pending integration is missing github options")
		}
		if pending.Options.GitHub.SenderGitHubUserID != user.GitHubUserID {
			return connect.NewError(connect.CodePermissionDenied, errors.New("this installation was started by a different GitHub user"))
		}
		if err := s.store.SetOrgGitHubInstallation(ctx, tx, caller.OrgID, req.InstallationId); err != nil {
			return err
		}
		if err := s.store.DeletePendingIntegration(ctx, tx, model.PendingIntegrationTypeGitHub, externalID); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		if connectErr, ok := errors.AsType[*connect.Error](err); ok {
			return nil, connectErr
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("github installation linked",
		"org_id", caller.OrgID,
		"installation_id", req.InstallationId,
		"user_id", caller.ID)
	return &xagentv1.LinkGitHubInstallationResponse{}, nil
}

func (s *Server) CreateGitHubToken(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
	caller := apiauth.Caller(ctx)
	if caller == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}
	if len(s.githubPrivateKey) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("GitHub App private key is not configured"))
	}
	org, err := s.store.GetOrg(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if org.GitHubInstallationID == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("no GitHub App installation linked to this organization"))
	}
	appID, err := strconv.ParseInt(s.githubAppID, 10, 64)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("invalid GitHub App ID: %w", err))
	}
	transport, err := ghinstallation.New(http.DefaultTransport, appID, org.GitHubInstallationID, s.githubPrivateKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create GitHub App transport: %w", err))
	}
	token, err := transport.Token(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create GitHub installation token: %w", err))
	}
	expiresAt, _, err := transport.Expiry()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get token expiry: %w", err))
	}
	return &xagentv1.CreateGitHubTokenResponse{
		Token:     token,
		ExpiresAt: timestamppb.New(expiresAt),
	}, nil
}
