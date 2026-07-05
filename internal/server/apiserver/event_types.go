package apiserver

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	"github.com/icholy/xagent/internal/eventrouter2"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

// GetEventTypes returns the eventrouter2 event-type registry so clients (the
// routing-rule editor UI) can discover the valid (source, type) event kinds and
// the attributes a rule may condition on for each. The registry is global —
// populated by the producer packages' init (githubserver, atlassianserver) — so
// the response is not org-scoped; the shipped DefaultRules are intentionally not
// exposed.
func (s *Server) GetEventTypes(ctx context.Context, req *xagentv1.GetEventTypesRequest) (*xagentv1.GetEventTypesResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpOrgRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read org"))
	}
	defs := eventrouter2.EventTypes()
	pb := make([]*xagentv1.EventTypeDef, len(defs))
	for i, def := range defs {
		pb[i] = &xagentv1.EventTypeDef{
			Source: def.Source,
			Type:   def.Type,
			Label:  def.Label,
			Attrs:  def.Attrs,
		}
	}
	return &xagentv1.GetEventTypesResponse{EventTypes: pb}, nil
}
