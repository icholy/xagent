// Package eventrouter2 is a greenfield reshaping of the event-routing core
// around attribute-based matching. Events carry a typed bag of matchable
// attributes and rules carry a list of (attr, op, value) conditions, so
// adding a new matchable dimension no longer requires a new field, matcher
// clause, and frontend special case throughout the stack.
//
// See proposals/draft/attribute-based-event-matching.md for the design.
package eventrouter2

// Attrs maps a dimension name to the event's values for that dimension.
// Single-valued dimensions are one-element slices.
type Attrs map[string][]string

// InputEvent is an external trigger to be matched against routing rules.
// Extractors parse source-specific syntax at extraction time and populate
// Attrs, so the matcher stays generic and carries no source knowledge.
type InputEvent struct {
	Source      string
	Type        string
	Description string
	Data        string // agent-visible payload, unchanged role
	URL         string
	UserID      string
	Attrs       Attrs // replaces Assignee and Values
	Meta        any
}

// Attr returns the event's values for a dimension. The "body" and "url"
// dimensions are derived views over Data and URL so extractors don't
// duplicate them; any other key reads from Attrs (nil/absent -> nil).
func (e InputEvent) Attr(key string) []string {
	switch key {
	case "body":
		return []string{e.Data}
	case "url":
		return []string{e.URL}
	default:
		return e.Attrs[key]
	}
}
