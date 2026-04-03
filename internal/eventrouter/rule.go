package eventrouter

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Rule defines a routing rule that determines whether an event should be routed to an org's tasks.
type Rule struct {
	Source  string `json:"source,omitempty"`
	Type    string `json:"type,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	Mention string `json:"mention,omitempty"`
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

// MarshalRules marshals a slice of Rules to JSON for database storage.
func MarshalRules(rules []Rule) (json.RawMessage, error) {
	return json.Marshal(rules)
}

// UnmarshalRules unmarshals JSON from the database into a slice of Rules.
func UnmarshalRules(data json.RawMessage) ([]Rule, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var rules []Rule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return rules, nil
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
