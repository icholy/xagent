package apiserver

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/store"
)

// AtlassianIntegration provides Atlassian-related operations needed by the API server.
type AtlassianIntegration interface {
	UnlinkAccount(ctx context.Context, userID string) error
	GetWebhookSecret(ctx context.Context, orgID int64) (string, error)
	WebhookURL(orgID int64) string
	GenerateWebhookSecret(ctx context.Context, orgID int64) (string, error)
}

// GitHubIntegration provides GitHub-related operations needed by the API server.
type GitHubIntegration interface {
	AppInstallURL() string
}

type Server struct {
	xagentv1connect.UnimplementedXAgentServiceHandler
	log       *slog.Logger
	store     *store.Store
	baseURL   string
	publisher pubsub.Publisher
	atlassian AtlassianIntegration
	github    GitHubIntegration
}

type Options struct {
	Log       *slog.Logger
	Store     *store.Store
	BaseURL   string
	Publisher pubsub.Publisher
	Atlassian AtlassianIntegration
	GitHub    GitHubIntegration
}

func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		log:       log,
		store:     opts.Store,
		baseURL:   opts.BaseURL,
		publisher: opts.Publisher,
		atlassian: opts.Atlassian,
		github:    opts.GitHub,
	}
}

func (s *Server) publish(userID string, n model.Notification) {
	if s.publisher == nil {
		return
	}
	n.UserID = userID
	if err := s.publisher.Publish(context.Background(), n); err != nil {
		s.log.Warn("failed to publish notification", "error", err, "type", n.Type, "resources", n.Resources)
	}
}

func (s *Server) Ping(ctx context.Context, req *xagentv1.PingRequest) (*xagentv1.PingResponse, error) {
	return &xagentv1.PingResponse{}, nil
}

func (s *Server) GetProfile(ctx context.Context, req *xagentv1.GetProfileRequest) (*xagentv1.GetProfileResponse, error) {
	u := apiauth.Caller(ctx)
	if u == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}
	orgs, err := s.store.ListOrgsByMember(ctx, nil, u.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	user, err := s.store.GetUser(ctx, nil, u.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &xagentv1.GetProfileResponse{
		Profile: &xagentv1.Profile{
			Id:    u.ID,
			Email: u.Email,
			Name:  u.Name,
		},
		DefaultOrgId:     user.DefaultOrgID,
		GithubAccount:    user.GitHubAccountProto(),
		AtlassianAccount: user.AtlassianAccountProto(),
	}
	resp.Orgs = make([]*xagentv1.Org, len(orgs))
	for i, o := range orgs {
		resp.Orgs[i] = o.Proto()
	}
	return resp, nil
}
