package eventrouter2

import (
	"slices"

	"github.com/icholy/xagent/internal/model"
)

// TranslateRule converts a legacy model.LegacyRoutingRule (flat matcher fields)
// into the equivalent set of new-shape RoutingRule values. It is the bridge for
// the translate-on-read cutover: the store uses it to decode pre-conditions
// stored rows into conditions-native rules.
//
// The result is a slice because the new shape requires every rule to name one
// concrete registered (Source, Type), and a condition is only valid on a type
// that emits its attr. A legacy rule with an empty Source and/or Type therefore
// expands to one v2 rule per applicable registered event type: those whose
// Source/Type match the legacy selector (empty = wildcard) and that emit every
// attr the rule's conditions reference. A rule whose condition attr no matching
// type emits produces zero rules, mirroring v1 where such a rule silently never
// matched.
func (r *SchemaRegistry) TranslateRule(rule model.LegacyRoutingRule) []RoutingRule {
	// Build the condition list from the legacy matcher fields, skipping empty
	// ones: an empty legacy field contributed no matcher clause in v1, so it
	// contributes no condition here.
	var conditions []Condition
	if rule.Prefix != "" {
		conditions = append(conditions, Condition{Attr: "body", Op: "prefix", Value: rule.Prefix})
	}
	if rule.Mention != "" {
		conditions = append(conditions, Condition{Attr: "mention", Op: "equals", Value: rule.Mention})
	}
	if rule.Assignee != "" {
		conditions = append(conditions, Condition{Attr: "assignee", Op: "equals", Value: rule.Assignee})
	}
	if rule.URLPrefix != "" {
		conditions = append(conditions, Condition{Attr: "url", Op: "prefix", Value: rule.URLPrefix})
	}
	if rule.Value != "" {
		conditions = append(conditions, Condition{Attr: "label", Op: "equals", Value: rule.Value})
	}

	var out []RoutingRule
	for _, def := range r.EventTypes() {
		if rule.Source != "" && rule.Source != def.Source {
			continue
		}
		if rule.Type != "" && rule.Type != def.Type {
			continue
		}
		if !emitsAll(def, conditions) {
			continue
		}
		out = append(out, RoutingRule{
			Source:     def.Source,
			Type:       def.Type,
			Conditions: conditions,
			Wakeup:     rule.Wakeup,
			Create:     rule.Create,
		})
	}
	return out
}

// emitsAll reports whether def emits every attr referenced by the conditions.
func emitsAll(def EventTypeDef, conditions []Condition) bool {
	for _, cond := range conditions {
		if !slices.Contains(def.Attrs, cond.Attr) {
			return false
		}
	}
	return true
}
