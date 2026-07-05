package eventrouter

import (
	"github.com/icholy/xagent/internal/eventrouter2"
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
