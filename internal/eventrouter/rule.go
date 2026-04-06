package eventrouter

import (
	"regexp"
	"strings"

	"github.com/icholy/xagent/internal/model"
)

// matchRule reports whether the rule matches the given event.
// Empty fields are treated as wildcards. For content matching,
// Prefix and Mention are checked against the event's Data field.
func matchRule(rule model.RoutingRule, event InputEvent) bool {
	if rule.Source != "" && rule.Source != event.Source {
		return false
	}
	if rule.Type != "" && rule.Type != event.Type {
		return false
	}
	if rule.Prefix != "" && !strings.HasPrefix(event.Data, rule.Prefix) {
		return false
	}
	if rule.Mention != "" && !matchMention(rule.Mention, event) {
		return false
	}
	return true
}

// matchMention checks whether body contains an @mention
// using platform-specific syntax.
func matchMention(mention string, event InputEvent) bool {
	switch event.Source {
	case "github":
		pattern := `(?i)(?:^|[\s(])@` + regexp.QuoteMeta(mention) + `(?:$|[\s,.)!?])`
		matched, _ := regexp.MatchString(pattern, event.Data)
		return matched
	case "atlassian":
		return strings.Contains(event.Data, "[~accountid:"+mention+"]")
	default:
		return false
	}
}
