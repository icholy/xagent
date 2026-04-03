package model

import (
	"encoding/json"
	"regexp"
	"strings"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

// RoutingRule defines a routing rule that determines whether an event
// should be routed to an org's tasks. Empty fields are treated as wildcards.
type RoutingRule struct {
	Source  string `json:"source,omitempty"`
	Type    string `json:"type,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	Mention string `json:"mention,omitempty"`
}

// Match reports whether the rule matches the given event.
// Empty fields are treated as wildcards. For content matching,
// Prefix and Mention are checked against the event data.
func (r *RoutingRule) Match(source, typ, data string) bool {
	if r.Source != "" && r.Source != source {
		return false
	}
	if r.Type != "" && r.Type != typ {
		return false
	}
	if r.Prefix != "" && !strings.HasPrefix(data, r.Prefix) {
		return false
	}
	if r.Mention != "" && !matchMention(source, data, r.Mention) {
		return false
	}
	return true
}

// Proto converts a RoutingRule to its protobuf representation.
func (r *RoutingRule) Proto() *xagentv1.RoutingRule {
	return &xagentv1.RoutingRule{
		Source:  r.Source,
		Type:    r.Type,
		Prefix:  r.Prefix,
		Mention: r.Mention,
	}
}

// RoutingRuleFromProto converts a protobuf RoutingRule to the model type.
func RoutingRuleFromProto(pb *xagentv1.RoutingRule) RoutingRule {
	return RoutingRule{
		Source:  pb.Source,
		Type:    pb.Type,
		Prefix:  pb.Prefix,
		Mention: pb.Mention,
	}
}

// MarshalRoutingRules marshals a slice of RoutingRules to JSON for database storage.
func MarshalRoutingRules(rules []RoutingRule) (json.RawMessage, error) {
	return json.Marshal(rules)
}

// UnmarshalRoutingRules unmarshals JSON from the database into a slice of RoutingRules.
func UnmarshalRoutingRules(data json.RawMessage) ([]RoutingRule, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var rules []RoutingRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// matchMention checks whether data contains a platform-specific @mention.
func matchMention(source, data, mention string) bool {
	switch source {
	case "github":
		pattern := `(?i)(?:^|[\s(])@` + regexp.QuoteMeta(mention) + `(?:$|[\s,.)!?])`
		matched, _ := regexp.MatchString(pattern, data)
		return matched
	case "atlassian":
		return strings.Contains(data, "[~accountid:"+mention+"]")
	default:
		return false
	}
}
