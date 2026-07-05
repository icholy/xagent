package apiserver

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	"github.com/icholy/xagent/internal/eventrouter"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

// GetEventTypes returns the eventrouter event-type registry so clients (the
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
	defs := eventrouter.DefaultSchemaRegistry.EventTypes()
	pb := make([]*xagentv1.EventTypeDef, len(defs))
	for i, def := range defs {
		attrs := make([]*xagentv1.AttrDef, len(def.Attrs))
		for j, attr := range def.Attrs {
			attrs[j] = &xagentv1.AttrDef{
				Key:         attr.Key,
				Label:       attr.Label,
				Help:        attr.Help,
				Placeholder: attr.Placeholder,
			}
		}
		pb[i] = &xagentv1.EventTypeDef{
			Source: def.Source,
			Type:   def.Type,
			Label:  def.Label,
			Attrs:  attrs,
		}
	}
	return &xagentv1.GetEventTypesResponse{EventTypes: pb}, nil
}
