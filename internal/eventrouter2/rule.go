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

// Match reports whether the rule matches the event. The matcher is generic:
// it never switches on the event source and does no source-specific parsing.
// Empty Source/Type are wildcards. Each condition holds when any value the
// event carries for cond.Attr satisfies (op, value); a condition on an attr
// the event does not carry fails. Conditions AND together.
func (r RoutingRule) Match(event InputEvent) bool {
	if r.Source != "" && r.Source != event.Source {
		return false
	}
	if r.Type != "" && r.Type != event.Type {
		return false
	}
	for _, cond := range r.Conditions {
		if !cond.Match(event.Attr(cond.Attr)) {
			return false
		}
	}
	return true
}

// Match reports whether any of the given values (the event's values for the
// condition's attr) satisfies the condition's op against its value. Passing an
// empty slice — an attr the event does not carry — always fails. Unknown ops
// never match.
func (c Condition) Match(values []string) bool {
	for _, v := range values {
		switch c.Op {
		case "equals":
			if v == c.Value {
				return true
			}
		case "prefix":
			if strings.HasPrefix(v, c.Value) {
				return true
			}
		case "contains":
			if strings.Contains(v, c.Value) {
				return true
			}
		}
	}
	return false
}
