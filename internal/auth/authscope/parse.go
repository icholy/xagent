package authscope

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Parse parses a scope from its wire form:
//
//	seg1.seg2.…:{json-predicates}
//
// The string is split on the FIRST colon: the operation path is colon-free, so
// everything left of it is the path (split on "." into segments) and everything
// right of it is a JSON object of predicates. The ":{…}" suffix is optional;
// absent ⇒ empty predicates.
//
// Predicate values must be JSON strings; numbers, booleans, arrays, and objects
// are rejected. (Set-valued predicates can be added later if needed.)
func Parse(s string) (Scope, error) {
	opRaw, predRaw, hasPred := strings.Cut(s, ":")
	op, err := parseOp(opRaw)
	if err != nil {
		return Scope{}, fmt.Errorf("parse scope %q: %w", s, err)
	}
	if !hasPred {
		// A nil Preds map is fine: ranging over it when matching is a no-op.
		return Scope{Op: op}, nil
	}
	preds, err := parsePreds(predRaw)
	if err != nil {
		return Scope{}, fmt.Errorf("parse scope %q: %w", s, err)
	}
	return Scope{Op: op, Preds: preds}, nil
}

// ParseSet parses each scope string and collects them into a Scopes, failing on
// the first malformed scope. A nil or empty input yields an empty Scopes.
func ParseSet(scopes []string) (Scopes, error) {
	if len(scopes) == 0 {
		return nil, nil
	}
	set := make(Scopes, len(scopes))
	for i, s := range scopes {
		parsed, err := Parse(s)
		if err != nil {
			return nil, err
		}
		set[i] = parsed
	}
	return set, nil
}

// parseOp splits the operation path into its dot-separated segments. Empty
// segments are rejected.
func parseOp(opRaw string) ([]string, error) {
	segs := strings.Split(opRaw, ".")
	if slices.Contains(segs, "") {
		return nil, fmt.Errorf("empty segment")
	}
	return segs, nil
}

// parsePreds unmarshals the predicate object. The top level must be a JSON
// object whose values are all strings; unmarshalling into map[string]string
// rejects any non-string value.
func parsePreds(s string) (map[string]string, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	var preds map[string]string
	if err := dec.Decode(&preds); err != nil {
		return nil, fmt.Errorf("predicates: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("predicates: trailing data")
	}
	if preds == nil {
		return nil, fmt.Errorf("predicates must be a JSON object")
	}
	return preds, nil
}

// String returns the wire form of the scope, the inverse of Parse. Scopes
// round-trip through Parse(s.String()). Predicate keys are emitted in sorted
// order.
func (s Scope) String() string {
	path := strings.Join(s.Op, ".")
	if len(s.Preds) == 0 {
		return path
	}
	b, _ := json.Marshal(s.Preds)
	return path + ":" + string(b)
}

// ValidScope reports whether s is a structurally valid scope. Validation is
// purely syntactic: it knows nothing about any operation taxonomy or attribute
// names, only that the scope parses (non-empty segments, non-empty
// alternatives, a well-formed object of string-valued predicates).
func ValidScope(s string) bool {
	_, err := Parse(s)
	return err == nil
}
