// Package scope implements a generic, semantically agnostic scope-matching
// engine for authorization. It assigns no meaning to operation segments or
// predicate keys: a scope is a pattern over an (operation-path, attributes)
// pair, and a target is the concrete operation-path and attributes an
// application builds per request. The application layer owns the operation
// taxonomy; this package only matches patterns.
//
// See proposals/draft/scope-based-permissions.md for the design.
package scope

import "slices"

// Scope is a single capability pattern. Op is the operation path, where each
// segment holds the set of allowed alternatives ("*" is a member that matches
// any segment). Preds maps an attribute key to the set of allowed values ("*"
// is a member meaning unconstrained). An empty Preds matches any instance of
// the operation.
type Scope struct {
	Op    [][]string
	Preds map[string][]string
}

// Target is the concrete operation path and attributes of a request, built by
// the application and tested against a Scope.
type Target struct {
	Op    []string
	Attrs map[string]string
}

// Set is a caller's held scopes. Authorize is an OR across them.
type Set []Scope

// segMatch reports whether seg is a member of the segment's allowed
// alternatives. "*" is a member that matches any single segment.
func segMatch(alts []string, seg string) bool {
	return slices.Contains(alts, "*") || slices.Contains(alts, seg)
}

// Matches reports whether the scope authorizes the target. The operation paths
// must have the same number of segments (each segment matches exactly one), and
// every predicate key in the scope must be satisfied (AND across keys, set
// membership within a key). An absent key or a "*" value is unconstrained; a
// constrained key whose attribute is missing from the target denies.
func (s Scope) Matches(t Target) bool {
	if len(s.Op) != len(t.Op) {
		return false
	}
	for i := range s.Op {
		if !segMatch(s.Op[i], t.Op[i]) {
			return false
		}
	}
	for key, allowed := range s.Preds {
		if slices.Contains(allowed, "*") {
			continue
		}
		got, ok := t.Attrs[key]
		if !ok || !slices.Contains(allowed, got) {
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
