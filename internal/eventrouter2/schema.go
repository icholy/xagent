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

// SchemaRegistry holds the set of registered event-type schemas and the routing
// state derived from them (the source:type index and the accumulated default
// rules). It is populated by MustRegister rather than a central table. The zero
// value (&SchemaRegistry{}) is ready to use; the index map is created lazily on
// first registration.
type SchemaRegistry struct {
	// eventTypes holds every registered schema in registration order. It is the
	// machine-readable contract that the router, rule validation, and (later) the
	// UI derive from, replacing hand-synced copies of the (source, type) pairs
	// and their applicable attrs.
	eventTypes []EventTypeDef

	// eventTypeByKey indexes eventTypes by "source:type" for O(1) lookup.
	eventTypeByKey map[string]EventTypeDef

	// defaultRules accumulates every registered schema's DefaultRules in
	// registration order, so DefaultRules is a plain lookup rather than a
	// per-call flatten.
	defaultRules []RoutingRule
}

// NewSchemaRegistry returns an empty registry ready for registration. It is
// equivalent to &SchemaRegistry{}; use whichever reads cleaner at the call site.
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{}
}

// MustRegister records def as the schema for its (Source, Type) event kind,
// making it available to EventTypeFor, EventTypes, and DefaultRules. It panics
// if a def with the same (Source, Type) is already registered.
func (r *SchemaRegistry) MustRegister(def EventTypeDef) {
	key := def.Source + ":" + def.Type
	if _, dup := r.eventTypeByKey[key]; dup {
		panic(fmt.Sprintf("eventrouter2: duplicate schema registration for source=%q type=%q", def.Source, def.Type))
	}
	if r.eventTypeByKey == nil {
		r.eventTypeByKey = map[string]EventTypeDef{}
	}
	r.eventTypes = append(r.eventTypes, def)
	r.eventTypeByKey[key] = def
	r.defaultRules = append(r.defaultRules, def.DefaultRules...)
}

// EventTypeFor returns the registry entry for a (source, type) pair, and false
// if none is registered.
func (r *SchemaRegistry) EventTypeFor(source, typ string) (EventTypeDef, bool) {
	def, ok := r.eventTypeByKey[source+":"+typ]
	return def, ok
}

// EventTypes returns every registered schema in registration order. It backs
// iteration over the registry and the GetEventTypes RPC.
func (r *SchemaRegistry) EventTypes() []EventTypeDef {
	return slices.Clone(r.eventTypes)
}

// DefaultRules returns every registered schema's DefaultRules in registration
// order, as accumulated during registration. It is the new-shape replacement
// for the legacy type-less {Prefix:"xagent:", Wakeup:true} fallback: rather than
// a validation special-case, the default set is an ordinary list of
// fully-defined rules contributed by the producers. These are not wired into the
// router here; that is a later layer.
func (r *SchemaRegistry) DefaultRules() []RoutingRule {
	return slices.Clone(r.defaultRules)
}

// Validate checks the rule against the event-type registry, returning a wrapped
// error naming the first offending selector, op, or attr. Every rule must name
// exactly one registered (Source, Type) event type: an empty Type is rejected,
// and — because the registry is keyed by (Source, Type) and label_added exists
// under multiple sources — an empty Source is rejected too. Each condition's op
// must be equals/prefix/contains, and its attr must be one of the selected event
// type's valid attrs (its Attrs set, which includes the derived body/url).
func (r *SchemaRegistry) Validate(rule RoutingRule) error {
	if rule.Type == "" {
		return fmt.Errorf("rule must select an event type: empty type")
	}
	if rule.Source == "" {
		return fmt.Errorf("rule must select an event source: empty source for type %q", rule.Type)
	}
	def, ok := r.EventTypeFor(rule.Source, rule.Type)
	if !ok {
		return fmt.Errorf("unknown event type: source=%q type=%q", rule.Source, rule.Type)
	}
	for _, cond := range rule.Conditions {
		switch cond.Op {
		case "equals", "prefix", "contains":
		default:
			return fmt.Errorf("unknown op %q on attr %q", cond.Op, cond.Attr)
		}
		if !slices.Contains(def.Attrs, cond.Attr) {
			return fmt.Errorf("attr %q not valid for event type %q/%q", cond.Attr, rule.Source, rule.Type)
		}
	}
	return nil
}

// DefaultSchemaRegistry is the process-wide registry the producer packages
// populate from their init functions. The apiserver GetEventTypes handler and
// the producer packages' own tests read from it.
var DefaultSchemaRegistry = &SchemaRegistry{}

// MustRegisterSchema records def on DefaultSchemaRegistry. It is the thin
// package-level entry point the producer packages (githubserver,
// atlassianserver) call from their init functions.
func MustRegisterSchema(def EventTypeDef) {
	DefaultSchemaRegistry.MustRegister(def)
}

// EventTypeFor returns the DefaultSchemaRegistry entry for a (source, type)
// pair, and false if none is registered.
func EventTypeFor(source, typ string) (EventTypeDef, bool) {
	return DefaultSchemaRegistry.EventTypeFor(source, typ)
}

// EventTypes returns every schema registered on DefaultSchemaRegistry in
// registration order.
func EventTypes() []EventTypeDef {
	return DefaultSchemaRegistry.EventTypes()
}
