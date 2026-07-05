package eventrouter2

import (
	"fmt"
	"slices"
)

// EventTypeDef declares a (Source, Type) event kind and the complete set of
// attribute dimensions a rule may condition on for that kind. Attrs is that full
// set: it always includes the derived views "body" and "url" (see
// InputEvent.Attr) plus the type's own emitted dimensions. DefaultRules holds
// the fully-defined rules the producer ships for this type (e.g. the "xagent:"
// body-prefix wakeup for comment types); DefaultRules aggregates them across
// every registered def.
type EventTypeDef struct {
	Source       string
	Type         string
	Label        string   // human label, e.g. "GitHub: Issue/PR Comment"
	Attrs        []string // complete valid attr set, including derived body/url
	DefaultRules []RoutingRule
}

// The registry state, populated by MustRegisterSchema from producer package
// init functions rather than a central table.
var (
	// eventTypes holds every registered schema in registration order. It is the
	// machine-readable contract that the router, rule validation, and (later) the
	// UI derive from, replacing hand-synced copies of the (source, type) pairs
	// and their applicable attrs.
	eventTypes []EventTypeDef

	// eventTypeByKey indexes eventTypes by "source:type" for O(1) lookup.
	eventTypeByKey = map[string]EventTypeDef{}

	// defaultRules accumulates every registered schema's DefaultRules in
	// registration order, so DefaultRules is a plain lookup rather than a
	// per-call flatten.
	defaultRules []RoutingRule
)

// MustRegisterSchema records def as the schema for its (Source, Type) event
// kind, making it available to EventTypeFor, EventTypes, and
// DefaultRules. It panics if a def with the same (Source, Type) is already
// registered. Intended to be called from a producer package's init.
func MustRegisterSchema(def EventTypeDef) {
	key := def.Source + ":" + def.Type
	if _, dup := eventTypeByKey[key]; dup {
		panic(fmt.Sprintf("eventrouter2: duplicate schema registration for source=%q type=%q", def.Source, def.Type))
	}
	eventTypes = append(eventTypes, def)
	eventTypeByKey[key] = def
	defaultRules = append(defaultRules, def.DefaultRules...)
}

// EventTypeFor returns the registry entry for a (source, type) pair, and false
// if none is registered.
func EventTypeFor(source, typ string) (EventTypeDef, bool) {
	def, ok := eventTypeByKey[source+":"+typ]
	return def, ok
}

// EventTypes returns every registered schema in registration order. It backs
// iteration over the registry and the future GetEventTypes RPC.
func EventTypes() []EventTypeDef {
	return slices.Clone(eventTypes)
}

// DefaultRules returns every registered schema's DefaultRules in registration
// order, as accumulated during registration. It is the new-shape replacement
// for the legacy type-less {Prefix:"xagent:", Wakeup:true} fallback: rather than
// a validation special-case, the default set is an ordinary list of
// fully-defined rules contributed by the producers. These are not wired into the
// router here; that is a later layer.
func DefaultRules() []RoutingRule {
	return slices.Clone(defaultRules)
}

// Validate checks the rule against the event-type registry, returning a wrapped
// error naming the first offending selector, op, or attr. Every rule must name
// exactly one registered (Source, Type) event type: an empty Type is rejected,
// and — because the registry is keyed by (Source, Type) and label_added exists
// under multiple sources — an empty Source is rejected too. Each condition's op
// must be equals/prefix/contains, and its attr must be one of the selected event
// type's valid attrs (its Attrs set, which includes the derived body/url).
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
		switch cond.Op {
		case "equals", "prefix", "contains":
		default:
			return fmt.Errorf("unknown op %q on attr %q", cond.Op, cond.Attr)
		}
		if !slices.Contains(def.Attrs, cond.Attr) {
			return fmt.Errorf("attr %q not valid for event type %q/%q", cond.Attr, r.Source, r.Type)
		}
	}
	return nil
}
