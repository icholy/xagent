package testx

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

// recordingT is a stand-in for assert.TestingT that records whether an
// assertion failed. FailNow calls runtime.Goexit (as *testing.T does) so a
// failing bounds check inside At stops before the slice is indexed.
type recordingT struct {
	failed bool
}

func (r *recordingT) FailNow()        { r.failed = true; runtime.Goexit() }
func (r *recordingT) Fail()           { r.failed = true }
func (r *recordingT) Log(args ...any) { _ = fmt.Sprint(args...) }

// runAssert runs fn against a recordingT in a goroutine (so a Goexit from
// FailNow only unwinds fn) and reports whether the assertion failed.
func runAssert(fn func(t assert.TestingT)) bool {
	rt := &recordingT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(rt)
	}()
	<-done
	return rt.failed
}

func TestAt(t *testing.T) {
	tests := []struct {
		name     string
		s        []string
		i        int
		wantFail bool
		want     string
	}{
		{"first of one", []string{"a"}, 0, false, "a"},
		{"first of many", []string{"a", "b", "c"}, 0, false, "a"},
		{"middle of many", []string{"a", "b", "c"}, 1, false, "b"},
		{"last of many", []string{"a", "b", "c"}, 2, false, "c"},
		{"empty out of bounds", nil, 0, true, ""},
		{"index past end", []string{"a", "b"}, 2, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			failed := runAssert(func(t assert.TestingT) {
				got = At(t, tt.s, tt.i)
			})
			assert.Equal(t, failed, tt.wantFail)
			if !tt.wantFail {
				assert.Equal(t, got, tt.want)
			}
		})
	}
}

func TestSafeSlice_ConcurrentAppend(t *testing.T) {
	// Arrange
	var s SafeSlice[int]
	var wg sync.WaitGroup

	// Act: append concurrently from many goroutines.
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Append(i)
		}()
	}
	wg.Wait()

	// Assert: every append is recorded exactly once.
	assert.Equal(t, s.Len(), 100)
	got := s.Slice()
	assert.Equal(t, len(got), 100)
	seen := map[int]bool{}
	for _, v := range got {
		seen[v] = true
	}
	assert.Equal(t, len(seen), 100)
}

func TestSafeSlice_SliceIsCopy(t *testing.T) {
	// Arrange
	var s SafeSlice[int]
	s.Append(1)

	// Act: mutating the returned slice must not affect later reads.
	got := s.Slice()
	got[0] = 99
	s.Append(2)

	// Assert
	assert.DeepEqual(t, s.Slice(), []int{1, 2})
}

func TestWaitFor(t *testing.T) {
	// Arrange: a condition that flips true after a few polls.
	n := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Act + Assert: returns once cond holds.
	WaitFor(t, ctx, func() bool {
		n++
		return n >= 3
	})
	assert.Assert(t, n >= 3)
}

func TestWaitFor_Timeout(t *testing.T) {
	// Arrange: a condition that never holds, against an expired deadline.
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	// Act: WaitFor should fail the test via Fatal, unwinding the goroutine.
	failed := runFatal(func(t testing.TB) {
		WaitFor(t, ctx, func() bool { return false })
	})

	// Assert
	assert.Assert(t, failed)
}

// fatalT is a stand-in for testing.TB that records a Fatalf and stops the
// goroutine via runtime.Goexit, like *testing.T.
type fatalT struct {
	testing.TB
	failed bool
}

func (f *fatalT) Helper() {}
func (f *fatalT) Fatalf(format string, args ...any) {
	f.failed = true
	_ = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func runFatal(fn func(t testing.TB)) bool {
	ft := &fatalT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(ft)
	}()
	<-done
	return ft.failed
}
