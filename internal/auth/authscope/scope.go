// Package authscope implements a generic, semantically agnostic scope-matching
// engine for authorization. It assigns no meaning to operation segments or
// predicate keys: a scope is a pattern over an (operation-path, attributes)
// pair, and a target is the concrete operation-path and attributes an
// application builds per request. The application layer owns the operation
// taxonomy; this package only matches patterns.
//
// See proposals/draft/scope-based-permissions.md for the design.
package authscope

import "slices"

// Scope is a single capability pattern. Op is the operation path, where each
// segment holds the set of allowed alternatives ("*" is a member that matches
// any segment). Preds maps an attribute key to its single allowed value. An
// absent key is unconstrained, so an empty Preds matches any instance of the
// operation; there is no predicate wildcard (a "*" value is matched literally).
type Scope struct {
	Op    [][]string
	Preds map[string]string
}

// Target is the concrete operation path and attributes of a request, built by
// the application and tested against a Scope.
type Target struct {
	Op    []string
	Attrs map[string]string
}

// Set is a caller's held scopes. Authorize is an OR across them.
type Set []Scope

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
		if got, ok := t.Attrs[key]; !ok || got != want {
			return false
		}
	}
	return true
}

// Authorize reports whether any held scope matches the target.
func (set Set) Authorize(t Target) bool {
	for _, s := range set {
		if s.Matches(t) {
			return true
		}
	}
	return false
}
