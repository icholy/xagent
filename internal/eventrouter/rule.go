package eventrouter

import (
	"regexp"
	"strings"
)

// Rule defines a routing rule that determines whether an event should be routed to an org's tasks.
type Rule struct {
	Source  string
	Type    string
	Prefix  string
	Mention string
}

// Match reports whether the rule matches the given event.
// Empty fields are treated as wildcards. For content matching,
// Prefix and Mention are checked against the event's Data field.
func (r *Rule) Match(event InputEvent) bool {
	if r.Source != "" && r.Source != event.Source {
		return false
	}
	if r.Type != "" && r.Type != event.Type {
		return false
	}
	if r.Prefix != "" && !strings.HasPrefix(event.Data, r.Prefix) {
		return false
	}
	if r.Mention != "" && !r.matchMention(event) {
		return false
	}
	return true
}

// matchMention checks whether body contains an @mention of r.Mention
// using platform-specific syntax.
func (r *Rule) matchMention(event InputEvent) bool {
	switch event.Source {
	case "github":
		pattern := `(?i)(?:^|[\s(])@` + regexp.QuoteMeta(r.Mention) + `(?:$|[\s,.)!?])`
		matched, _ := regexp.MatchString(pattern, event.Data)
		return matched
	case "atlassian":
		return strings.Contains(event.Data, "[~accountid:"+r.Mention+"]")
	default:
		return false
	}
}
