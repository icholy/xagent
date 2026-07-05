package eventrouter2

import "fmt"

// EventTypeDef declares a (Source, Type) event kind and the attribute
// dimensions it emits. Attrs lists the attr keys this event type carries
// BEYOND the always-present derived views "body" and "url" (see InputEvent.Attr).
type EventTypeDef struct {
	Source string
	Type   string
	Label  string   // human label, e.g. "GitHub: Issue/PR Comment"
	Attrs  []string // attr keys this event type emits, beyond body/url
}

// EventTypes is the registry of event kinds the webhook extractors emit. It is
// the machine-readable contract that the router, rule validation, and (later)
// the UI derive from, replacing hand-synced copies of the (source, type) pairs
// and their applicable attrs. The Attrs of each entry mirror the extractor
// mapping in the design's §1.
var EventTypes = []EventTypeDef{
	{Source: "github", Type: "issue_comment", Label: "GitHub: Issue/PR Comment", Attrs: []string{"mention"}},
	{Source: "github", Type: "pull_request_review_comment", Label: "GitHub: PR Review Comment", Attrs: []string{"mention"}},
	{Source: "github", Type: "pull_request_review", Label: "GitHub: PR Review", Attrs: []string{"mention"}},
	{Source: "github", Type: "issue_assigned", Label: "GitHub: Issue Assigned", Attrs: []string{"assignee"}},
	{Source: "github", Type: "pull_request_assigned", Label: "GitHub: PR Assigned", Attrs: []string{"assignee"}},
	{Source: "github", Type: "pull_request_opened", Label: "GitHub: PR Opened", Attrs: nil},
	{Source: "github", Type: "pull_request_closed", Label: "GitHub: PR Closed", Attrs: []string{"state"}},
	{Source: "github", Type: "label_added", Label: "GitHub: Label Added", Attrs: []string{"label"}},
	{Source: "atlassian", Type: "comment_created", Label: "Jira: Issue Comment", Attrs: []string{"mention"}},
	{Source: "atlassian", Type: "label_added", Label: "Jira: Label Added", Attrs: []string{"label"}},
}

// EventTypeFor returns the registry entry for a (source, type) pair, and false
// if none is registered.
func EventTypeFor(source, typ string) (EventTypeDef, bool) {
	for _, def := range EventTypes {
		if def.Source == source && def.Type == typ {
			return def, true
		}
	}
	return EventTypeDef{}, false
}

// derivedAttrs are always valid on any rule: they are views over the event's
// Data and URL that every event carries, not per-type dimensions.
var derivedAttrs = map[string]bool{"body": true, "url": true}

// validOps are the comparison operators a Condition may use, matching
// Condition.Match.
var validOps = map[string]bool{"equals": true, "prefix": true, "contains": true}

// knownAttr reports whether key is a valid attr in the universe of any rule:
// a derived view (body/url) or an attr emitted by some registered event type.
func knownAttr(key string) bool {
	if derivedAttrs[key] {
		return true
	}
	for _, def := range EventTypes {
		for _, a := range def.Attrs {
			if a == key {
				return true
			}
		}
	}
	return false
}

// typeEmitsAttr reports whether def carries key: a derived view (always) or one
// of the type's declared attrs.
func typeEmitsAttr(def EventTypeDef, key string) bool {
	if derivedAttrs[key] {
		return true
	}
	for _, a := range def.Attrs {
		if a == key {
			return true
		}
	}
	return false
}

// Validate checks the rule against the event-type registry, returning a wrapped
// error naming the first offending selector, op, or attr. Every rule must name
// exactly one registered (Source, Type) event type: an empty Type is rejected,
// and — because the registry is keyed by (Source, Type) and label_added exists
// under multiple sources — an empty Source is rejected too. Each condition's op
// must be equals/prefix/contains, and its attr must be one the selected event
// type emits, i.e. a declared attr of that type or an always-valid derived view
// (body/url).
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
		if !validOps[cond.Op] {
			return fmt.Errorf("unknown op %q on attr %q", cond.Op, cond.Attr)
		}
		if !knownAttr(cond.Attr) {
			return fmt.Errorf("unknown attr %q", cond.Attr)
		}
		if !typeEmitsAttr(def, cond.Attr) {
			return fmt.Errorf("attr %q not emitted by event type %q", cond.Attr, r.Type)
		}
	}
	return nil
}
