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
	"github.com/icholy/xagent/internal/server/atlassianserver"
	"github.com/icholy/xagent/internal/server/githubserver"
	"github.com/icholy/xagent/internal/store"
)

type Server struct {
	xagentv1connect.UnimplementedXAgentServiceHandler
	log       *slog.Logger
	store     *store.Store
	baseURL   string
	publisher pubsub.Publisher
	atlassian *atlassianserver.Server
	github    *githubserver.Server
}

type Options struct {
	Log       *slog.Logger
	Store     *store.Store
	BaseURL   string
	Publisher pubsub.Publisher
	Atlassian *atlassianserver.Server
	GitHub    *githubserver.Server
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

func (s *Server) publish(n model.Notification) {
	if s.publisher == nil {
		return
	}
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
	return &xagentv1.GetProfileResponse{
		Profile: &xagentv1.Profile{
			Id:    u.ID,
			Email: u.Email,
			Name:  u.Name,
		},
		DefaultOrgId:     user.DefaultOrgID,
		GithubAccount:    user.GitHubAccountProto(),
		AtlassianAccount: user.AtlassianAccountProto(),
		Orgs:             model.MapProtos(orgs),
	}, nil
}
