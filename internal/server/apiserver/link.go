package apiserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

func (s *Server) CreateLink(ctx context.Context, req *xagentv1.CreateLinkRequest) (*xagentv1.CreateLinkResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpTaskWrite) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot write task"))
	}
	// Verify task ownership
	ok, err := s.store.HasTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %d not found", req.TaskId))
	}
	link := &model.Link{
		TaskID:     req.TaskId,
		Relevance:  req.Relevance,
		URL:        req.Url,
		RoutingKey: model.RoutingKey(req.Url),
		Title:      req.Title,
		Subscribe:  req.Subscribe,
		CreatedAt:  time.Now(),
	}
	if err := s.store.CreateLink(ctx, nil, link); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("link created", "task", req.TaskId, "relevance", req.Relevance, "url", req.Url)
	s.publish(model.Notification{
		Type: "change",
		Resources: []model.NotificationResource{
			{Action: "created", Type: "task_links", ID: req.TaskId},
			{Action: "created", Type: "link", ID: link.ID},
		},
		OrgID:    caller.OrgID,
		UserID:   caller.ID,
		ClientID: caller.ClientID,
		Time:     time.Now(),
	})
	return &xagentv1.CreateLinkResponse{
		Link: link.Proto(),
	}, nil
}

func (s *Server) ListLinks(ctx context.Context, req *xagentv1.ListLinksRequest) (*xagentv1.ListLinksResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpTaskRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read task"))
	}
	links, err := s.store.ListLinksByTask(ctx, nil, req.TaskId, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListLinksResponse{
		Links: model.ProtoMap(links),
	}, nil
}
