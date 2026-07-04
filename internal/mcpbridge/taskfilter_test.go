package mcpbridge

import (
	"testing"

	"gotest.tools/v3/assert"
)

// TestTaskFilter_DefaultDeliversEverything: a fresh filter mutes nothing.
func TestTaskFilter_DefaultDeliversEverything(t *testing.T) {
	t.Parallel()
	f := NewTaskFilter()
	assert.Assert(t, !f.All())
	assert.Assert(t, !f.Muted(1))
	assert.Equal(t, len(f.Exceptions()), 0)
}

// TestTaskFilter_Blocklist: without mute-all the exception set is a
// blocklist of muted ids.
func TestTaskFilter_Blocklist(t *testing.T) {
	t.Parallel()
	f := NewTaskFilter()
	f.Mute(3)
	f.Mute(1)
	assert.Assert(t, f.Muted(1))
	assert.Assert(t, f.Muted(3))
	assert.Assert(t, !f.Muted(2))
	assert.DeepEqual(t, f.Exceptions(), []int64{1, 3})

	f.Unmute(1)
	assert.Assert(t, !f.Muted(1))
	assert.DeepEqual(t, f.Exceptions(), []int64{3})
}

// TestTaskFilter_MuteAllThenUnmuteOne: under mute-all the exception set
// inverts into an allowlist of still-delivering tasks.
func TestTaskFilter_MuteAllThenUnmuteOne(t *testing.T) {
	t.Parallel()
	f := NewTaskFilter()
	f.MuteAll()
	assert.Assert(t, f.All())
	assert.Assert(t, f.Muted(1))
	assert.Assert(t, f.Muted(999))
	assert.Equal(t, len(f.Exceptions()), 0)

	f.Unmute(123)
	assert.Assert(t, !f.Muted(123)) // punched a hole
	assert.Assert(t, f.Muted(7))    // everything else stays muted
	assert.DeepEqual(t, f.Exceptions(), []int64{123})
}

// TestTaskFilter_RemuteUnderMuteAll: muting an unmuted exception removes it,
// so the task is muted with the rest again.
func TestTaskFilter_RemuteUnderMuteAll(t *testing.T) {
	t.Parallel()
	f := NewTaskFilter()
	f.MuteAll()
	f.Unmute(123)
	assert.Assert(t, !f.Muted(123))

	f.Mute(123)
	assert.Assert(t, f.Muted(123))
	assert.Equal(t, len(f.Exceptions()), 0)
}

// TestTaskFilter_Clear resets back to the deliver-everything default.
func TestTaskFilter_Clear(t *testing.T) {
	t.Parallel()
	f := NewTaskFilter()
	f.MuteAll()
	f.Unmute(1)
	f.Clear()
	assert.Assert(t, !f.All())
	assert.Assert(t, !f.Muted(1))
	assert.Assert(t, !f.Muted(2))
	assert.Equal(t, len(f.Exceptions()), 0)
}
