package testx

import (
	"fmt"
	"runtime"
	"testing"

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
