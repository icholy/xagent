// Package authscope implements a generic, semantically agnostic scope-matching
// engine for authorization. It assigns no meaning to operation segments or
// predicate keys: a scope is a pattern over an (operation-path, attributes)
// pair, and a target is the concrete operation-path and attributes an
// application builds per request. The application layer owns the operation
// taxonomy; this package only matches patterns.
//
// See proposals/draft/scope-based-permissions.md for the design.
package authscope

import (
	"slices"
	"strconv"
)

// Scope is a single capability pattern. Op is the operation path, where each
// segment holds the set of allowed alternatives ("*" is a member that matches
// any segment). Preds maps an attribute key to its single allowed value. An
// absent key is unconstrained, so an empty Preds matches any instance of the
// operation; there is no predicate wildcard (a "*" value is matched literally).
type Scope struct {
	Op    [][]string
	Preds map[string]string
}

// Attr is a single concrete attribute of a target: an application attribute key
// and its value, already stringified.
type Attr struct {
	Name  string
	Value string
}

// Int64Attr builds an Attr from an int64 value, centralizing the int->string
// conversion so call sites never repeat strconv.FormatInt.
func Int64Attr(name string, v int64) Attr {
	return Attr{Name: name, Value: strconv.FormatInt(v, 10)}
}

// StringAttr builds an Attr from a string value.
func StringAttr(name, v string) Attr {
	return Attr{Name: name, Value: v}
}

// Target is the concrete operation path and attributes of a request, built by
// the application and tested against a Scope.
type Target struct {
	Op    []string
	Attrs []Attr
}

// Targeter is implemented by anything that can produce a Target. It is the
// construction seam: typed, domain-specific target values implement it so call
// sites don't assemble Target literals by hand. Matching still operates on the
// concrete Target, so the algorithm is unchanged.
type Targeter interface {
	Target() Target
}

// Target satisfies Targeter by returning itself, so a plain Target value flows
// through wherever a Targeter is expected.
func (t Target) Target() Target { return t }

// Set is a caller's held scopes. Authorize is an OR across them.
type Set []Scope

// AdminScope is the global-admin wildcard grant: any two-segment operation on
// any instance. It is the capability half of today's omnipotent-within-org
// caller. AdminScope is taxonomy-agnostic — it depends only on the operation
// arity (two segments), not on what the segments mean.
const AdminScope = "*.*"

// Admin returns a Set containing only AdminScope, the global-admin grant.
func Admin() Set {
	s, err := Parse(AdminScope)
	if err != nil {
		// AdminScope is a compile-time constant and always parses.
		panic(err)
	}
	return Set{s}
}

// Matches reports whether the scope authorizes the target. The operation paths
// must have the same number of segments (each segment matches exactly one), and
// every predicate key in the scope must equal the target's attribute (AND across
// keys). An absent key is unconstrained; a key whose attribute is missing from
// the target, or whose value differs, denies.
func (s Scope) Matches(t Target) bool {
	if len(s.Op) != len(t.Op) {
		return false
	}
	for i, alts := range s.Op {
		// "*" is a member that matches any single segment.
		if !slices.Contains(alts, "*") && !slices.Contains(alts, t.Op[i]) {
			return false
		}
	}
	for key, want := range s.Preds {
		got, ok := attrValue(t.Attrs, key)
		if !ok || got != want {
			return false
		}
	}
	return true
}

// attrValue returns the value of the first attr with the given name. Targets are
// only built by the typed Targeter values, which never emit a duplicate name, so
// first-match-wins needs no guard.
func attrValue(attrs []Attr, name string) (string, bool) {
	for _, a := range attrs {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

// Authorize reports whether any held scope matches the target. It accepts a
// Targeter so typed target values can be passed directly; a plain Target value
// satisfies Targeter and flows through unchanged.
func (set Set) Authorize(tr Targeter) bool {
	t := tr.Target()
	for _, s := range set {
		if s.Matches(t) {
			return true
		}
	}
	return false
}
