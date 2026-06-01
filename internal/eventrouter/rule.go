package eventrouter

import (
	"regexp"
	"slices"
	"strings"

	"github.com/icholy/xagent/internal/model"
)

// MatchRule reports whether the rule matches the event.
// Empty fields are treated as wildcards. URLPrefix matches against the
// event's URL; Prefix and Mention are checked against the event's Data field;
// Value is checked for membership in the event's Values.
func (e InputEvent) MatchRule(rule model.RoutingRule) bool {
	if rule.Source != "" && rule.Source != e.Source {
		return false
	}
	if rule.Type != "" && rule.Type != e.Type {
		return false
	}
	if rule.URLPrefix != "" && !strings.HasPrefix(e.URL, rule.URLPrefix) {
		return false
	}
	if rule.Prefix != "" && !strings.HasPrefix(e.Data, rule.Prefix) {
		return false
	}
	if rule.Value != "" && !slices.Contains(e.Values, rule.Value) {
		return false
	}
	if rule.Mention != "" && !e.matchMention(rule.Mention) {
		return false
	}
	if rule.Assignee != "" && !e.matchAssignee(rule.Assignee) {
		return false
	}
	return true
}

// matchMention checks whether the event data contains an @mention
// using platform-specific syntax.
func (e InputEvent) matchMention(mention string) bool {
	switch e.Source {
	case "github":
		pattern := `(?i)(?:^|[\s(])@` + regexp.QuoteMeta(mention) + `(?:$|[\s,.)!?])`
		matched, _ := regexp.MatchString(pattern, e.Data)
		return matched
	case "atlassian":
		return strings.Contains(e.Data, "[~accountid:"+mention+"]")
	default:
		return false
	}
}

// matchAssignee checks whether the event's assignee matches the rule's
// configured assignee. A rule with Assignee set only matches events whose
// InputEvent.Assignee is non-empty (i.e. assignment events).
func (e InputEvent) matchAssignee(assignee string) bool {
	if e.Assignee == "" {
		return false
	}
	switch e.Source {
	case "github":
		return strings.EqualFold(e.Assignee, assignee)
	case "atlassian":
		// Jira assignment matching is deferred — extractor does not emit
		// assignment events yet.
		return false
	default:
		return false
	}
}
