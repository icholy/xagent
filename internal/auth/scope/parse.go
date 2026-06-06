package scope

import (
	"encoding/json"
	"fmt"
	"strconv"
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
// Predicate values are normalized to sets of strings: a JSON scalar becomes a
// singleton set, a JSON array becomes the set of its stringified elements.
func Parse(s string) (Scope, error) {
	path := s
	predStr := ""
	hasColon := false
	if i := strings.IndexByte(s, ':'); i >= 0 {
		hasColon = true
		path, predStr = s[:i], s[i+1:]
	}
	op, err := parsePath(path)
	if err != nil {
		return Scope{}, fmt.Errorf("parse scope %q: %w", s, err)
	}
	preds := map[string][]string{}
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

// parsePreds unmarshals the predicate object and normalizes every value to a
// set of strings. The top level must be a JSON object; scalars become singleton
// sets and arrays become the set of their stringified elements.
func parsePreds(s string) (map[string][]string, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("predicates: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("predicates: trailing data")
	}
	if raw == nil {
		return nil, fmt.Errorf("predicates must be a JSON object")
	}
	preds := make(map[string][]string, len(raw))
	for k, v := range raw {
		set, err := normValue(v)
		if err != nil {
			return nil, fmt.Errorf("predicate %q: %w", k, err)
		}
		preds[k] = set
	}
	return preds, nil
}

// normValue normalizes a JSON predicate value to a set of strings. Arrays
// become the set of their stringified elements; scalars become singleton sets.
// An empty array is rejected as an empty alternative set.
func normValue(v any) ([]string, error) {
	if arr, ok := v.([]any); ok {
		if len(arr) == 0 {
			return nil, fmt.Errorf("empty value set")
		}
		out := make([]string, len(arr))
		for i, e := range arr {
			s, err := scalarString(e)
			if err != nil {
				return nil, err
			}
			out[i] = s
		}
		return out, nil
	}
	s, err := scalarString(v)
	if err != nil {
		return nil, err
	}
	return []string{s}, nil
}

// scalarString stringifies a JSON scalar (string, number, or bool). Any other
// type (null, object, nested array) is rejected.
func scalarString(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case json.Number:
		return x.String(), nil
	case bool:
		return strconv.FormatBool(x), nil
	default:
		return "", fmt.Errorf("invalid value type %T", v)
	}
}

// String returns the wire form of the scope, the inverse of Parse. Scopes
// round-trip through Parse(s.String()). Predicate values are always emitted as
// JSON arrays of strings with keys in sorted order.
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
// alternatives, a well-formed predicate object).
func ValidScope(s string) bool {
	_, err := Parse(s)
	return err == nil
}
