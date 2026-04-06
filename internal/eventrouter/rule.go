package eventrouter

import (
	"regexp"
	"strings"

	"github.com/icholy/xagent/internal/model"
)

// MatchRule reports whether the rule matches the event.
// Empty fields are treated as wildcards. For content matching,
// Prefix and Mention are checked against the event's Data field.
func (e InputEvent) MatchRule(rule model.RoutingRule) bool {
	if rule.Source != "" && rule.Source != e.Source {
		return false
	}
	if rule.Type != "" && rule.Type != e.Type {
		return false
	}
	if rule.Prefix != "" && !strings.HasPrefix(e.Data, rule.Prefix) {
		return false
	}
	if rule.Mention != "" && !e.matchMention(rule.Mention) {
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
