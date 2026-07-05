package eventrouter

import (
	"cmp"

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

// matchesV2 reports whether the legacy rule matches the event via the
// attribute-based matcher, using the router's schema registry (nil-defaulting to
// eventrouter2.DefaultSchemaRegistry so production sites need not set it).
// reg.TranslateRule expands the flat legacy rule into one new-shape rule per
// applicable registered event type; the legacy rule matches iff any of them
// matches. An empty translation (a rule whose conditions no registered type
// emits) never matches, mirroring the legacy behavior.
func (r *Router) matchesV2(rule model.RoutingRule, event eventrouter2.InputEvent) bool {
	reg := cmp.Or(r.Registry, eventrouter2.DefaultSchemaRegistry)
	for _, v2 := range reg.TranslateRule(rule) {
		if v2.Match(event) {
			return true
		}
	}
	return false
}
