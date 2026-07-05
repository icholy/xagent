package eventrouter

import (
	"github.com/icholy/xagent/internal/eventrouter2"
	"github.com/icholy/xagent/internal/model"
)

// toV2 converts the event into the eventrouter2 shape for attribute-based
// matching. It is a field copy: the legacy Assignee/Values fields are not
// carried over — those dimensions now travel in Attrs (populated by the webhook
// handlers), which is what the new matcher reads. Attrs converts directly since
// eventrouter.Attrs and eventrouter2.Attrs are the same underlying type.
func (e InputEvent) toV2() eventrouter2.InputEvent {
	return eventrouter2.InputEvent{
		Source:      e.Source,
		Type:        e.Type,
		Description: e.Description,
		Data:        e.Data,
		URL:         e.URL,
		UserID:      e.UserID,
		Attrs:       eventrouter2.Attrs(e.Attrs),
		Meta:        e.Meta,
	}
}

// ruleToV2 converts a conditions-native model.RoutingRule into the
// eventrouter2.RoutingRule matcher shape. It is a 1:1 field copy: the store has
// already translated any legacy stored rows into conditions on read, so the
// matcher no longer fans out per event type — a stored rule maps to exactly one
// matcher rule.
func ruleToV2(rule model.RoutingRule) eventrouter2.RoutingRule {
	var conds []eventrouter2.Condition
	for _, c := range rule.Conditions {
		conds = append(conds, eventrouter2.Condition{Attr: c.Attr, Op: c.Op, Value: c.Value})
	}
	return eventrouter2.RoutingRule{
		Source:     rule.Source,
		Type:       rule.Type,
		Conditions: conds,
		Wakeup:     rule.Wakeup,
		Create:     rule.Create,
	}
}

// matchesV2 reports whether the conditions-native rule matches the event via the
// attribute-based matcher. The store returns conditions-native rules (legacy
// rows are translated on read), so this is a direct 1:1 conversion plus Match —
// no match-time fan-out.
func (r *Router) matchesV2(rule model.RoutingRule, event eventrouter2.InputEvent) bool {
	return ruleToV2(rule).Match(event)
}
