// Package testx provides small, general-purpose helpers for tests built on
// gotest.tools/v3/assert.
package testx

import (
	"context"
	"sync"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

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

// SafeSlice is a concurrency-safe append-only slice, handy for collecting values
// from goroutines under test. The zero value is ready to use.
type SafeSlice[T any] struct {
	mu sync.Mutex
	s  []T
}

// Append adds v to the slice.
func (s *SafeSlice[T]) Append(v T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s = append(s.s, v)
}

// Slice returns a copy of the current contents.
func (s *SafeSlice[T]) Slice() []T {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]T(nil), s.s...)
}

// Len returns the number of elements.
func (s *SafeSlice[T]) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.s)
}

// WaitFor polls cond every millisecond until it returns true, failing the test
// (via t.Fatal) if ctx is done first. Pass a ctx with a deadline to bound the
// wait.
func WaitFor(t testing.TB, ctx context.Context, cond func() bool) {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if cond() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("condition not met before deadline: %v", ctx.Err())
		case <-tick.C:
		}
	}
}

// WaitForWithTimeout is WaitFor bounded by timeout: it derives a child context
// from ctx with the given timeout and polls cond until it holds, failing the
// test if the timeout (or ctx) expires first.
func WaitForWithTimeout(t testing.TB, ctx context.Context, timeout time.Duration, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	WaitFor(t, ctx, cond)
}
