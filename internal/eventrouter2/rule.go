package eventrouter2

import (
	"strings"

	"github.com/icholy/xagent/internal/model"
)

// RoutingRule matches events by a (Source, Type) selector plus a list of
// attribute conditions. Empty Source/Type are wildcards; the action side
// (Wakeup, Create) is unchanged from the legacy rule.
type RoutingRule struct {
	Source     string
	Type       string
	Conditions []Condition
	Wakeup     bool
	Create     *model.CreateTaskAction
}

// Condition constrains one attribute dimension of an event. Op is one of
// "equals", "prefix", or "contains"; comparisons are literal (exact,
// case-sensitive) string operations.
type Condition struct {
	Attr  string
	Op    string
	Value string
}

// MatchRule reports whether the rule matches the event. The matcher is
// generic: it never switches on the event source and does no source-specific
// parsing. Empty Source/Type are wildcards. Each condition holds when any
// value the event carries for cond.Attr satisfies (op, value); a condition on
// an attr the event does not carry fails. Conditions AND together.
func MatchRule(rule RoutingRule, event InputEvent) bool {
	if rule.Source != "" && rule.Source != event.Source {
		return false
	}
	if rule.Type != "" && rule.Type != event.Type {
		return false
	}
	for _, cond := range rule.Conditions {
		if !matchCondition(cond, event.Attr(cond.Attr)) {
			return false
		}
	}
	return true
}

// matchCondition reports whether any of the event's values for the condition's
// attr satisfies the condition's op against its value.
func matchCondition(cond Condition, values []string) bool {
	for _, v := range values {
		if matchOp(cond.Op, v, cond.Value) {
			return true
		}
	}
	return false
}

// matchOp applies a single literal string operator. Unknown ops never match.
func matchOp(op, value, want string) bool {
	switch op {
	case "equals":
		return value == want
	case "prefix":
		return strings.HasPrefix(value, want)
	case "contains":
		return strings.Contains(value, want)
	default:
		return false
	}
}
