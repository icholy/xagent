package eventrouter2

import (
	"fmt"
	"slices"
)

// EventTypeDef declares a (Source, Type) event kind and the complete set of
// attribute dimensions a rule may condition on for that kind. Attrs is that full
// set: it always includes the derived views "body" and "url" (see
// InputEvent.Attr) plus the type's own emitted dimensions.
type EventTypeDef struct {
	Source string
	Type   string
	Label  string   // human label, e.g. "GitHub: Issue/PR Comment"
	Attrs  []string // complete valid attr set, including derived body/url
}

// EventTypes is the registry of event kinds the webhook extractors emit. It is
// the machine-readable contract that the router, rule validation, and (later)
// the UI derive from, replacing hand-synced copies of the (source, type) pairs
// and their applicable attrs. Each entry's Attrs is the complete valid set for
// that type — the derived body/url plus the emitted dimensions from the design's
// §1 extractor mapping.
var EventTypes = []EventTypeDef{
	{
		Source: "github",
		Type:   "issue_comment",
		Label:  "GitHub: Issue/PR Comment",
		Attrs:  []string{"body", "url", "mention"},
	},
	{
		Source: "github",
		Type:   "pull_request_review_comment",
		Label:  "GitHub: PR Review Comment",
		Attrs:  []string{"body", "url", "mention"},
	},
	{
		Source: "github",
		Type:   "pull_request_review",
		Label:  "GitHub: PR Review",
		Attrs:  []string{"body", "url", "mention"},
	},
	{
		Source: "github",
		Type:   "issue_assigned",
		Label:  "GitHub: Issue Assigned",
		Attrs:  []string{"body", "url", "assignee"},
	},
	{
		Source: "github",
		Type:   "pull_request_assigned",
		Label:  "GitHub: PR Assigned",
		Attrs:  []string{"body", "url", "assignee"},
	},
	{
		Source: "github",
		Type:   "pull_request_opened",
		Label:  "GitHub: PR Opened",
		Attrs:  []string{"body", "url"},
	},
	{
		Source: "github",
		Type:   "pull_request_closed",
		Label:  "GitHub: PR Closed",
		Attrs:  []string{"body", "url", "state"},
	},
	{
		Source: "github",
		Type:   "label_added",
		Label:  "GitHub: Label Added",
		Attrs:  []string{"body", "url", "label"},
	},
	{
		Source: "atlassian",
		Type:   "comment_created",
		Label:  "Jira: Issue Comment",
		Attrs:  []string{"body", "url", "mention"},
	},
	{
		Source: "atlassian",
		Type:   "label_added",
		Label:  "Jira: Label Added",
		Attrs:  []string{"body", "url", "label"},
	},
}

// eventTypeByKey indexes EventTypes by "source:type" for O(1) lookup. Populated
// once in init.
var eventTypeByKey = map[string]EventTypeDef{}

func init() {
	for _, def := range EventTypes {
		eventTypeByKey[def.Source+":"+def.Type] = def
	}
}

// EventTypeFor returns the registry entry for a (source, type) pair, and false
// if none is registered.
func EventTypeFor(source, typ string) (EventTypeDef, bool) {
	def, ok := eventTypeByKey[source+":"+typ]
	return def, ok
}

// Validate checks the rule against the event-type registry, returning a wrapped
// error naming the first offending selector, op, or attr. Every rule must name
// exactly one registered (Source, Type) event type: an empty Type is rejected,
// and — because the registry is keyed by (Source, Type) and label_added exists
// under multiple sources — an empty Source is rejected too. Each condition's op
// must be equals/prefix/contains, and its attr must be one of the selected event
// type's valid attrs (its Attrs set, which includes the derived body/url).
func (r RoutingRule) Validate() error {
	if r.Type == "" {
		return fmt.Errorf("rule must select an event type: empty type")
	}
	if r.Source == "" {
		return fmt.Errorf("rule must select an event source: empty source for type %q", r.Type)
	}
	def, ok := EventTypeFor(r.Source, r.Type)
	if !ok {
		return fmt.Errorf("unknown event type: source=%q type=%q", r.Source, r.Type)
	}
	for _, cond := range r.Conditions {
		switch cond.Op {
		case "equals", "prefix", "contains":
		default:
			return fmt.Errorf("unknown op %q on attr %q", cond.Op, cond.Attr)
		}
		if !slices.Contains(def.Attrs, cond.Attr) {
			return fmt.Errorf("attr %q not valid for event type %q/%q", cond.Attr, r.Source, r.Type)
		}
	}
	return nil
}
