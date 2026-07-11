package apiserver

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
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
// runs the shared matcher (Router.Plan) and the subscribed-link lookup, and
// reports which rule matched and which tasks would wake or be created — without
// writing any events or tasks rows. Fire mode (fire=true) is a later layer; the
// request's fire flag is ignored here and Fired is always false.
func (s *Server) TestEvent(ctx context.Context, req *xagentv1.TestEventRequest) (*xagentv1.TestEventResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if !caller.Scopes.Allow(authscope.OpOrgRead) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot read org"))
	}

	// Validate (source, type) against the registry and reject unknown attr keys,
	// mirroring the contract SchemaRegistry.Validate enforces for rules: a typo'd
	// attr must fail loudly as a non-match rather than be silently ignored. details
	// keys are deliberately not validated — they're free-form source-defined
	// context (§2a).
	def, ok := eventrouter.DefaultSchemaRegistry.EventTypeFor(req.Source, req.Type)
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("unknown event type: source=%q type=%q", req.Source, req.Type))
	}
	validAttr := map[string]bool{}
	for _, attr := range def.Attrs {
		validAttr[attr.Key] = true
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
		if !validAttr[key] {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("attr %q not valid for event type %q/%q", key, req.Source, req.Type))
		}
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
	router := &eventrouter.Router{Log: s.log, Store: s.store, Publisher: s.publisher}
	matches, err := router.Plan(ctx, input)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Links are stored under routing_key = model.RoutingKey(url); derive the key the
	// same way so a non-canonical event URL resolves to the same key as the link.
	key := model.RoutingKey(input.URL)
	var pbMatches []*xagentv1.TestEventMatch
	for _, match := range matches {
		// Scope to the caller's org (§4): Plan evaluates every org the caller is a
		// member of, but a test event is explicitly about the one org the operator
		// is looking at.
		if match.OrgID != caller.OrgID {
			continue
		}

		// Same subscribed-link lookup Route uses to decide wake vs. create: links
		// present -> the subscribed tasks would wake; otherwise a task would be
		// created if the matched rule opts in.
		linksByOrg, err := s.store.FindSubscribedLinksForOrgs(ctx, nil, key, []int64{match.OrgID})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		links := linksByOrg[match.OrgID]

		var wakeTasks []*xagentv1.TestEventTask
		seen := map[int64]bool{}
		for _, link := range links {
			if seen[link.TaskID] {
				continue
			}
			seen[link.TaskID] = true
			task, err := s.store.GetTask(ctx, nil, link.TaskID, match.OrgID)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			wakeTasks = append(wakeTasks, &xagentv1.TestEventTask{Id: task.ID, Name: task.Name})
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
			WakeTasks:   wakeTasks,
			WouldCreate: len(links) == 0 && match.Rule.Create != nil,
		})
	}

	return &xagentv1.TestEventResponse{Matches: pbMatches, Fired: false}, nil
}
