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

// TestEvent feeds a hand-composed synthetic event into the real routing code
// scoped to the caller's org (see proposals/draft/test-event-injection.md). This
// is the dry-run path (OpOrgRead): it composes an InputEvent from the request,
// runs the shared matcher (Router.Plan), and reports which rule matched and
// whether it would wake or create — without writing any events or tasks rows.
// Fire mode (fire=true) is a later layer; the request's fire flag is ignored
// here and Fired is always false.
//
// The request is not validated against the schema registry: attrs pass through
// as-is, and an unknown source/type/attr simply yields no match rather than an
// error. Task/link resolution is intentionally skipped — the report is derived
// from the matched rule alone.
func (s *Server) TestEvent(ctx context.Context, req *xagentv1.TestEventRequest) (*xagentv1.TestEventResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpOrgRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read org"))
	}
	if s.router == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("event router not configured"))
	}

	// Compose the synthetic InputEvent. The "body" and "url" attrs are the derived
	// views over Data/URL (InputEvent.Attr); every other attr is wrapped into the
	// one-element slice the matcher expects. Details pass through verbatim — the
	// router does not interpret them and they never affect matching. Orgs is scoped
	// to the caller's org; Meta stays nil (only OnRouteOutcome reads it, which the
	// test path never invokes).
	input := eventrouter.InputEvent{
		Source:      req.Source,
		Type:        req.Type,
		Description: req.Description,
		UserID:      caller.ID,
		Orgs:        []int64{caller.OrgID},
		Details:     req.Details,
	}
	for key, value := range req.Attrs {
		switch key {
		case "body":
			input.Data = value
		case "url":
			input.URL = value
		default:
			if input.Attrs == nil {
				input.Attrs = eventrouter.Attrs{}
			}
			input.Attrs[key] = []string{value}
		}
	}

	// Plan is the shared read-only matcher — running it (not a parallel simulator)
	// is what guarantees the dry-run report agrees with what firing would do. No
	// OnRouteOutcome: dry run never applies anything.
	matches, err := s.router.Plan(ctx, input)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var pbMatches []*xagentv1.TestEventMatch
	for _, match := range matches {
		// Scope to the caller's org (§4): Plan evaluates every org the caller is a
		// member of, but a test event is explicitly about the one org the operator
		// is looking at.
		if match.OrgID != caller.OrgID {
			continue
		}

		// A default-rule match has no configured index; report it as -1 (the proto
		// contract for the shipped default) rather than its position in the defaults.
		ruleIndex := int32(match.RuleIndex)
		if match.RuleDefault {
			ruleIndex = -1
		}
		pbMatches = append(pbMatches, &xagentv1.TestEventMatch{
			OrgId:       match.OrgID,
			RuleIndex:   ruleIndex,
			WouldWake:   match.Rule.Wakeup,
			WouldCreate: match.Rule.Create != nil,
		})
	}

	return &xagentv1.TestEventResponse{Matches: pbMatches, Fired: false}, nil
}
