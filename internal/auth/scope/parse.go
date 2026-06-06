package scope

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Parse parses a scope from its wire form:
//
//	seg1.seg2.…:{json-predicates}
//
// The string is split on the FIRST colon: the operation path is colon-free, so
// everything left of it is the path (split on "." into segments, each split on
// "|" into the set of alternatives) and everything right of it is a JSON object
// of predicates. The ":{…}" suffix is optional; absent ⇒ empty predicates.
//
// Predicate values must be JSON strings; numbers, booleans, arrays, and objects
// are rejected. (Set-valued predicates can be added later if needed.)
func Parse(s string) (Scope, error) {
	path, predStr, hasColon := strings.Cut(s, ":")
	op, err := parsePath(path)
	if err != nil {
		return Scope{}, fmt.Errorf("parse scope %q: %w", s, err)
	}
	preds := map[string]string{}
	if hasColon {
		preds, err = parsePreds(predStr)
		if err != nil {
			return Scope{}, fmt.Errorf("parse scope %q: %w", s, err)
		}
	}
	return Scope{Op: op, Preds: preds}, nil
}

// parsePath splits the operation path into segments, each into its set of
// "|"-separated alternatives. Empty segments and empty alternatives are
// rejected.
func parsePath(path string) ([][]string, error) {
	segs := strings.Split(path, ".")
	op := make([][]string, len(segs))
	for i, seg := range segs {
		if seg == "" {
			return nil, fmt.Errorf("empty segment")
		}
		alts := strings.Split(seg, "|")
		for _, a := range alts {
			if a == "" {
				return nil, fmt.Errorf("empty alternative in segment %q", seg)
			}
		}
		op[i] = alts
	}
	return op, nil
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
	segs := make([]string, len(s.Op))
	for i, alts := range s.Op {
		segs[i] = strings.Join(alts, "|")
	}
	path := strings.Join(segs, ".")
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
