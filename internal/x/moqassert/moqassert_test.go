package moqassert

import (
	"fmt"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"
)

// recordingT is a stand-in for assert.TestingT that records whether an
// assertion failed. FailNow calls runtime.Goexit (as *testing.T does) so a
// failing bounds check inside CallN stops before the slice is indexed.
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

func TestCalledTimes(t *testing.T) {
	tests := []struct {
		name     string
		calls    []string
		want     int
		wantFail bool
	}{
		{"zero match", nil, 0, false},
		{"zero mismatch", nil, 1, true},
		{"one match", []string{"a"}, 1, false},
		{"one mismatch", []string{"a"}, 2, true},
		{"many match", []string{"a", "b", "c"}, 3, false},
		{"many mismatch", []string{"a", "b", "c"}, 2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := runAssert(func(t assert.TestingT) {
				CalledTimes(t, tt.calls, tt.want)
			})
			assert.Equal(t, failed, tt.wantFail)
		})
	}
}

func TestCalled(t *testing.T) {
	tests := []struct {
		name     string
		calls    []string
		wantFail bool
	}{
		{"zero", nil, true},
		{"one", []string{"a"}, false},
		{"many", []string{"a", "b"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := runAssert(func(t assert.TestingT) {
				Called(t, tt.calls)
			})
			assert.Equal(t, failed, tt.wantFail)
		})
	}
}

func TestNotCalled(t *testing.T) {
	tests := []struct {
		name     string
		calls    []string
		wantFail bool
	}{
		{"zero", nil, false},
		{"one", []string{"a"}, true},
		{"many", []string{"a", "b"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := runAssert(func(t assert.TestingT) {
				NotCalled(t, tt.calls)
			})
			assert.Equal(t, failed, tt.wantFail)
		})
	}
}

func TestCallN(t *testing.T) {
	tests := []struct {
		name     string
		calls    []string
		n        int
		wantFail bool
		want     string
	}{
		{"first of one", []string{"a"}, 0, false, "a"},
		{"first of many", []string{"a", "b", "c"}, 0, false, "a"},
		{"last of many", []string{"a", "b", "c"}, 2, false, "c"},
		{"empty out of bounds", nil, 0, true, ""},
		{"index past end", []string{"a", "b"}, 2, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			failed := runAssert(func(t assert.TestingT) {
				got = CallN(t, tt.calls, tt.n)
			})
			assert.Equal(t, failed, tt.wantFail)
			if !tt.wantFail {
				assert.Equal(t, got, tt.want)
			}
		})
	}
}
