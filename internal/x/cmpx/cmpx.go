// Package cmpx provides field-selection helpers for
// github.com/google/go-cmp, intended for use with the project's existing
// gotest.tools/v3/assert.DeepEqual (which accepts cmp.Option values).
//
// The central helper, OnlyFields, is the inverse of cmpopts.IgnoreFields:
// instead of ignoring the named fields it ignores everything except them.
// go-cmp has no built-in for this; see the long-standing open issue
// google/go-cmp#66 ("Add IgnoreFieldsExcept"). It is implemented the same
// way IgnoreFields is internally — cmp.FilterPath combined with cmp.Ignore()
// — but with the predicate negated.
//
// Known limitation: OnlyFields does not replicate IgnoreFields's embedded-field
// promotion semantics. Names promoted from embedded structs may not resolve;
// it targets flat and explicitly-nested field paths (dot-delimited, e.g.
// "Inner.Foo").
package cmpx

import (
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp"
)

// OnlyFields is the inverse of cmpopts.IgnoreFields: it compares only the
// named fields of struct type T, ignoring all others. Names may be
// dot-delimited to target nested fields (e.g. "Inner.Foo").
func OnlyFields[T any](names ...string) cmp.Option {
	t := reflect.TypeFor[T]()
	keep := make(map[string]bool, len(names))
	for _, n := range names {
		keep[n] = true
	}
	return cmp.FilterPath(func(p cmp.Path) bool {
		path, inScope := fieldPath(p, t)
		if !inScope {
			return false
		}
		if path == "" {
			return false
		}
		return !keptOrAncestor(path, keep)
	}, cmp.Ignore())
}

// fieldPath returns the dot-joined struct-field names from the point where
// type t appears in the path down to the current step.
func fieldPath(p cmp.Path, t reflect.Type) (string, bool) {
	var parts []string
	inScope := false
	for _, s := range p {
		if !inScope {
			if s.Type() == t {
				inScope = true
			}
			continue
		}
		if sf, ok := s.(cmp.StructField); ok {
			parts = append(parts, sf.Name())
		}
	}
	if !inScope {
		return "", false
	}
	return strings.Join(parts, "."), true
}

// keptOrAncestor reports whether path is a kept field, or a prefix of one
// (so we descend into "Inner" when "Inner.Foo" is requested).
func keptOrAncestor(path string, keep map[string]bool) bool {
	if keep[path] {
		return true
	}
	for k := range keep {
		if strings.HasPrefix(k, path+".") {
			return true
		}
	}
	return false
}
