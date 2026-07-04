// Package testx provides small, general-purpose helpers for tests built on
// gotest.tools/v3/assert.
package testx

import "gotest.tools/v3/assert"

// At returns s[i], failing the test (via t.Fatal) if the slice has fewer than
// i+1 elements, so the caller can safely chain assertions on the returned
// value.
func At[T any](t assert.TestingT, s []T, i int) T {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}
	assert.Assert(t, i < len(s), "wanted index %d, only %d elements", i, len(s))
	return s[i]
}
