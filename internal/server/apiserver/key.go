package apiserver

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

func (s *Server) CreateKey(ctx context.Context, req *xagentv1.CreateKeyRequest) (*xagentv1.CreateKeyResponse, error) {
	caller := apiauth.MustCaller(ctx)
	allowed := caller.Scopes.Allow(authscope.OpKeyCreate)
	if !allowed {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot create key"))
	}
	rawKey, err := apiauth.GenerateKey()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t := req.ExpiresAt.AsTime()
		expiresAt = &t
	}
	key := &model.Key{
		ID:        uuid.NewString(),
		Name:      req.Name,
		TokenHash: apiauth.HashKey(rawKey),
		OrgID:     caller.OrgID,
		ExpiresAt: expiresAt,
		// Every key is admin (*.*) for now; there is intentionally no way to
		// choose narrower scopes yet (later phase).
		// TODO: limit API keys to a smaller scope surface, and expose
		// UI-configurable scopes here if a need arises.
		Scopes: authscope.Admin(),
	}
	if err := s.store.CreateKey(ctx, nil, key); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("key created", "id", key.ID, "org_id", caller.OrgID)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "created", Type: "keys"}},
		OrgID:     caller.OrgID,
		UserID:    caller.ID,
		ClientID:  caller.ClientID,
		Time:      time.Now(),
	})
	return &xagentv1.CreateKeyResponse{
		Key:      key.Proto(),
		RawToken: rawKey,
	}, nil
}

func (s *Server) ListKeys(ctx context.Context, req *xagentv1.ListKeysRequest) (*xagentv1.ListKeysResponse, error) {
	caller := apiauth.MustCaller(ctx)
	allowed := caller.Scopes.Allow(authscope.OpKeyRead)
	if !allowed {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot list keys"))
	}
	keys, err := s.store.ListKeys(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListKeysResponse{
		Keys: model.ProtoMap(keys),
	}, nil
}

func (s *Server) DeleteKey(ctx context.Context, req *xagentv1.DeleteKeyRequest) (*xagentv1.DeleteKeyResponse, error) {
	caller := apiauth.MustCaller(ctx)
	allowed := caller.Scopes.Allow(authscope.OpKeyWrite)
	if !allowed {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot write key"))
	}
	if err := s.store.DeleteKey(ctx, nil, req.Id, caller.OrgID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("key deleted", "id", req.Id)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "deleted", Type: "keys"}},
		OrgID:     caller.OrgID,
		UserID:    caller.ID,
		ClientID:  caller.ClientID,
		Time:      time.Now(),
	})
	return &xagentv1.DeleteKeyResponse{}, nil
}
