// Package moqassert provides bounds-safe helpers for asserting on the call
// logs recorded by moq-generated mocks. moq emits a Calls() accessor per
// mocked method that returns a slice of structs holding the recorded
// arguments; these helpers take that slice directly and assert on it using
// gotest.tools/v3/assert.
package moqassert

import "gotest.tools/v3/assert"

// CalledTimes asserts the mock method was called exactly want times.
func CalledTimes[T any](t assert.TestingT, calls []T, want int) {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}
	assert.Equal(t, len(calls), want)
}

// Called asserts the mock method was called at least once.
func Called[T any](t assert.TestingT, calls []T) {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}
	assert.Assert(t, len(calls) > 0, "expected at least one call, got none")
}

// NotCalled asserts the mock method was never called.
func NotCalled[T any](t assert.TestingT, calls []T) {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}
	assert.Equal(t, len(calls), 0)
}

// CallN returns the args of the nth call (0-indexed), failing the test
// (via t.Fatal) if there are fewer than n+1 calls, so the caller can
// safely chain further assertions on the returned struct.
func CallN[T any](t assert.TestingT, calls []T, n int) T {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}
	assert.Assert(t, n < len(calls), "wanted call %d, only %d recorded", n, len(calls))
	return calls[n]
}
