package model

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestTaskStatus_Label(t *testing.T) {
	t.Parallel()
	// The unspecified (zero) status renders empty — e.g. a freshly created task
	// has no prior status.
	assert.Equal(t, TaskStatusUnspecified.Label(), "")
	assert.Equal(t, TaskStatusPending.Label(), "Pending")
	assert.Equal(t, TaskStatusCancelled.Label(), "Cancelled")
}

func TestTask_ApplyRunnerEvent(t *testing.T) {
	tests := []struct {
		name    string
		before  Task
		after   Task
		event   RunnerEvent
		changed bool
	}{
		// Version handling
		{
			name: "version mismatch returns false",
			before: Task{
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
				Version: 2,
			},
			event: RunnerEvent{
				Event:   RunnerEventStarted,
				Version: 1,
			},
			changed: false,
		},
		{
			name: "version 0 bypasses check",
			before: Task{
				Status:  TaskStatusRunning,
				Version: 5,
			},
			after: Task{
				Status:  TaskStatusFailed,
				Version: 5,
			},
			event: RunnerEvent{
				Event:   RunnerEventFailed,
				Version: 0,
			},
			changed: true,
		},

		// Started events
		{
			name: "started: pending with restart -> running",
			before: Task{
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
			},
			after: Task{
				Status: TaskStatusRunning,
			},
			event: RunnerEvent{
				Event: RunnerEventStarted,
			},
			changed: true,
		},
		{
			name: "started: restarting with restart -> running",
			before: Task{
				Status:  TaskStatusRestarting,
				Command: TaskCommandRestart,
			},
			after: Task{
				Status: TaskStatusRunning,
			},
			event: RunnerEvent{
				Event: RunnerEventStarted,
			},
			changed: true,
		},
		{
			name: "started: running with restart -> running (SIGHUP case)",
			before: Task{
				Status:  TaskStatusRunning,
				Command: TaskCommandRestart,
			},
			after: Task{
				Status: TaskStatusRunning,
			},
			event: RunnerEvent{
				Event: RunnerEventStarted,
			},
			changed: true,
		},
		{
			name: "started: pending without restart command returns false",
			before: Task{
				Status: TaskStatusPending,
			},
			event: RunnerEvent{
				Event: RunnerEventStarted,
			},
			changed: false,
		},
		{
			name: "started: cancelled -> cancelling with stop",
			before: Task{
				Status: TaskStatusCancelled,
			},
			after: Task{
				Status:  TaskStatusCancelling,
				Command: TaskCommandStop,
				Version: 1,
			},
			event: RunnerEvent{
				Event: RunnerEventStarted,
			},
			changed: true,
		},
		{
			name: "started: archived completed -> cancelling with stop",
			before: Task{
				Status:   TaskStatusCompleted,
				Archived: true,
			},
			after: Task{
				Status:   TaskStatusCancelling,
				Command:  TaskCommandStop,
				Version:  1,
				Archived: true,
			},
			event: RunnerEvent{
				Event: RunnerEventStarted,
			},
			changed: true,
		},
		{
			name: "started: completed status returns false",
			before: Task{
				Status: TaskStatusCompleted,
			},
			event: RunnerEvent{
				Event: RunnerEventStarted,
			},
			changed: false,
		},

		// Started events with start command
		{
			name: "started: pending with start -> running",
			before: Task{
				Status:  TaskStatusPending,
				Command: TaskCommandStart,
			},
			after: Task{
				Status: TaskStatusRunning,
			},
			event: RunnerEvent{
				Event: RunnerEventStarted,
			},
			changed: true,
		},
		{
			name: "started: running with start -> running",
			before: Task{
				Status:  TaskStatusRunning,
				Command: TaskCommandStart,
			},
			after: Task{
				Status: TaskStatusRunning,
			},
			event: RunnerEvent{
				Event: RunnerEventStarted,
			},
			changed: true,
		},

		// Stopped events
		{
			name: "stopped: running with no command -> completed",
			before: Task{
				Status: TaskStatusRunning,
			},
			after: Task{
				Status: TaskStatusCompleted,
			},
			event: RunnerEvent{
				Event: RunnerEventStopped,
			},
			changed: true,
		},
		{
			name: "stopped: running with stop command -> cancelled",
			before: Task{
				Status:  TaskStatusRunning,
				Command: TaskCommandStop,
			},
			after: Task{
				Status: TaskStatusCancelled,
			},
			event: RunnerEvent{
				Event: RunnerEventStopped,
			},
			changed: true,
		},
		{
			name: "stopped: cancelling with stop command -> cancelled",
			before: Task{
				Status:  TaskStatusCancelling,
				Command: TaskCommandStop,
			},
			after: Task{
				Status: TaskStatusCancelled,
			},
			event: RunnerEvent{
				Event: RunnerEventStopped,
			},
			changed: true,
		},
		{
			name: "stopped: running with restart command returns false",
			before: Task{
				Status:  TaskStatusRunning,
				Command: TaskCommandRestart,
			},
			event: RunnerEvent{
				Event: RunnerEventStopped,
			},
			changed: false,
		},
		{
			name: "stopped: running with start command -> pending (ready to restart)",
			before: Task{
				Status:  TaskStatusRunning,
				Command: TaskCommandStart,
			},
			after: Task{
				Status:  TaskStatusPending,
				Command: TaskCommandStart,
			},
			event: RunnerEvent{
				Event: RunnerEventStopped,
			},
			changed: true,
		},
		{
			name: "stopped: pending status returns false",
			before: Task{
				Status: TaskStatusPending,
			},
			event: RunnerEvent{
				Event: RunnerEventStopped,
			},
			changed: false,
		},

		// Failed events
		{
			name: "failed: pending -> failed",
			before: Task{
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
			},
			after: Task{
				Status: TaskStatusFailed,
			},
			event: RunnerEvent{
				Event: RunnerEventFailed,
			},
			changed: true,
		},
		{
			name: "failed: restarting -> failed",
			before: Task{
				Status:  TaskStatusRestarting,
				Command: TaskCommandRestart,
			},
			after: Task{
				Status: TaskStatusFailed,
			},
			event: RunnerEvent{
				Event: RunnerEventFailed,
			},
			changed: true,
		},
		{
			name: "failed: running -> failed",
			before: Task{
				Status: TaskStatusRunning,
			},
			after: Task{
				Status: TaskStatusFailed,
			},
			event: RunnerEvent{
				Event: RunnerEventFailed,
			},
			changed: true,
		},
		{
			name: "failed: running with stop command -> failed",
			before: Task{
				Status:  TaskStatusRunning,
				Command: TaskCommandStop,
			},
			after: Task{
				Status: TaskStatusFailed,
			},
			event: RunnerEvent{
				Event: RunnerEventFailed,
			},
			changed: true,
		},
		{
			name: "failed: cancelling -> failed",
			before: Task{
				Status:  TaskStatusCancelling,
				Command: TaskCommandStop,
			},
			after: Task{
				Status: TaskStatusFailed,
			},
			event: RunnerEvent{
				Event: RunnerEventFailed,
			},
			changed: true,
		},
		{
			name: "failed: already failed returns false",
			before: Task{
				Status: TaskStatusFailed,
			},
			event: RunnerEvent{
				Event: RunnerEventFailed,
			},
			changed: false,
		},
		{
			name: "failed: completed returns false",
			before: Task{
				Status: TaskStatusCompleted,
			},
			event: RunnerEvent{
				Event: RunnerEventFailed,
			},
			changed: false,
		},
		{
			name: "failed: cancelled returns false",
			before: Task{
				Status: TaskStatusCancelled,
			},
			event: RunnerEvent{
				Event: RunnerEventFailed,
			},
			changed: false,
		},

		// Unknown event type
		{
			name: "unknown event type returns false",
			before: Task{
				Status: TaskStatusRunning,
			},
			event: RunnerEvent{
				Event: RunnerEventType("unknown"),
			},
			changed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.before
			got := task.ApplyRunnerEvent(&tt.event)
			assert.Equal(t, got, tt.changed)
			if tt.changed {
				assert.DeepEqual(t, task, tt.after)
			}
		})
	}
}

func TestTask_ApplyLifecycleEvent(t *testing.T) {
	actor := UserActor("tester")
	tests := []struct {
		name    string
		before  Task
		kind    LifecycleKind
		message string
		after   Task              // expected task state (only checked when it applies)
		want    *LifecyclePayload // nil means the guard rejected the transition
	}{
		{
			name:   "created records landing status with no prior status",
			before: Task{Status: TaskStatusPending},
			kind:   LifecycleKindCreated,
			after:  Task{Status: TaskStatusPending},
			want:   &LifecyclePayload{Kind: LifecycleKindCreated, Actor: actor, FromStatus: "", ToStatus: "Pending"},
		},
		{
			name:   "updated is not a status transition (from == to)",
			before: Task{Status: TaskStatusRunning},
			kind:   LifecycleKindUpdated,
			after:  Task{Status: TaskStatusRunning},
			want:   &LifecyclePayload{Kind: LifecycleKindUpdated, Actor: actor, FromStatus: "Running", ToStatus: "Running"},
		},
		{
			name:   "cancelled from running",
			before: Task{Status: TaskStatusRunning},
			kind:   LifecycleKindCancelled,
			after:  Task{Status: TaskStatusCancelling, Command: TaskCommandStop, Version: 1},
			want:   &LifecyclePayload{Kind: LifecycleKindCancelled, Actor: actor, FromStatus: "Running", ToStatus: "Cancelling"},
		},
		{
			name:   "cancelled rejected from completed",
			before: Task{Status: TaskStatusCompleted},
			kind:   LifecycleKindCancelled,
			want:   nil,
		},
		{
			name:   "restarted reuses Start: completed -> pending",
			before: Task{Status: TaskStatusCompleted},
			kind:   LifecycleKindRestarted,
			after:  Task{Status: TaskStatusPending, Command: TaskCommandStart, Version: 1},
			want:   &LifecyclePayload{Kind: LifecycleKindRestarted, Actor: actor, FromStatus: "Completed", ToStatus: "Pending"},
		},
		{
			name:   "restarted reuses Start: running keeps status, sets start command",
			before: Task{Status: TaskStatusRunning},
			kind:   LifecycleKindRestarted,
			after:  Task{Status: TaskStatusRunning, Command: TaskCommandStart, Version: 1},
			want:   &LifecyclePayload{Kind: LifecycleKindRestarted, Actor: actor, FromStatus: "Running", ToStatus: "Running"},
		},
		{
			name:   "restarted rejected from pending",
			before: Task{Status: TaskStatusPending},
			kind:   LifecycleKindRestarted,
			want:   nil,
		},
		{
			name:   "archived from completed",
			before: Task{Status: TaskStatusCompleted},
			kind:   LifecycleKindArchived,
			after:  Task{Status: TaskStatusCompleted, Archived: true},
			want:   &LifecyclePayload{Kind: LifecycleKindArchived, Actor: actor, FromStatus: "Completed", ToStatus: "Completed"},
		},
		{
			name:   "archived rejected from running",
			before: Task{Status: TaskStatusRunning},
			kind:   LifecycleKindArchived,
			want:   nil,
		},
		{
			name:   "auto-archived from cancelled",
			before: Task{Status: TaskStatusCancelled},
			kind:   LifecycleKindAutoArchived,
			after:  Task{Status: TaskStatusCancelled, Archived: true},
			want:   &LifecyclePayload{Kind: LifecycleKindAutoArchived, Actor: actor, FromStatus: "Cancelled", ToStatus: "Cancelled"},
		},
		{
			name:   "auto-archived rejected from pending",
			before: Task{Status: TaskStatusPending},
			kind:   LifecycleKindAutoArchived,
			want:   nil,
		},
		{
			name:   "unarchived clears archived flag",
			before: Task{Status: TaskStatusCompleted, Archived: true},
			kind:   LifecycleKindUnarchived,
			after:  Task{Status: TaskStatusCompleted, Archived: false},
			want:   &LifecyclePayload{Kind: LifecycleKindUnarchived, Actor: actor, FromStatus: "Completed", ToStatus: "Completed"},
		},
		{
			name:   "unarchived rejected when not archived",
			before: Task{Status: TaskStatusCompleted},
			kind:   LifecycleKindUnarchived,
			want:   nil,
		},
		{
			name:   "sandbox started: pending with restart -> running",
			before: Task{Status: TaskStatusPending, Command: TaskCommandRestart},
			kind:   LifecycleKindSandboxStarted,
			after:  Task{Status: TaskStatusRunning},
			want:   &LifecyclePayload{Kind: LifecycleKindSandboxStarted, Actor: actor, FromStatus: "Pending", ToStatus: "Running"},
		},
		{
			name:   "sandbox started rejected from completed",
			before: Task{Status: TaskStatusCompleted},
			kind:   LifecycleKindSandboxStarted,
			want:   nil,
		},
		{
			name:   "sandbox exited: running -> completed",
			before: Task{Status: TaskStatusRunning},
			kind:   LifecycleKindSandboxExited,
			after:  Task{Status: TaskStatusCompleted},
			want:   &LifecyclePayload{Kind: LifecycleKindSandboxExited, Actor: actor, FromStatus: "Running", ToStatus: "Completed"},
		},
		{
			name:    "sandbox failed: running -> failed carries message",
			before:  Task{Status: TaskStatusRunning},
			kind:    LifecycleKindSandboxFailed,
			message: "container failed",
			after:   Task{Status: TaskStatusFailed},
			want:    &LifecyclePayload{Kind: LifecycleKindSandboxFailed, Actor: actor, FromStatus: "Running", ToStatus: "Failed", Message: "container failed"},
		},
		{
			name:   "unspecified kind is rejected",
			before: Task{Status: TaskStatusRunning},
			kind:   LifecycleKindUnspecified,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.before
			got, ok := task.ApplyLifecycleEvent(tt.kind, actor, tt.message)
			if tt.want == nil {
				// A rejected guard leaves the task untouched and returns no payload.
				assert.Assert(t, !ok)
				assert.Assert(t, got == nil)
				assert.DeepEqual(t, task, tt.before)
				return
			}
			assert.Assert(t, ok)
			assert.DeepEqual(t, got, tt.want)
			assert.DeepEqual(t, task, tt.after)
		})
	}
}

func TestTask_IsDone(t *testing.T) {
	tests := []struct {
		status TaskStatus
		want   bool
	}{
		{TaskStatusPending, false},
		{TaskStatusRunning, false},
		{TaskStatusRestarting, false},
		{TaskStatusCancelling, false},
		{TaskStatusCompleted, true},
		{TaskStatusFailed, true},
		{TaskStatusCancelled, true},
	}
	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			task := Task{Status: tt.status}
			assert.Equal(t, task.IsDone(), tt.want)
		})
	}
}

func TestTask_Archive(t *testing.T) {
	tests := []struct {
		name   string
		before Task
		after  Task
		want   bool
	}{
		{
			name:   "from completed succeeds",
			before: Task{Status: TaskStatusCompleted},
			after:  Task{Status: TaskStatusCompleted, Archived: true},
			want:   true,
		},
		{
			name:   "from failed succeeds",
			before: Task{Status: TaskStatusFailed},
			after:  Task{Status: TaskStatusFailed, Archived: true},
			want:   true,
		},
		{
			name:   "from pending fails",
			before: Task{Status: TaskStatusPending},
			after:  Task{Status: TaskStatusPending},
			want:   false,
		},
		{
			name:   "from running fails",
			before: Task{Status: TaskStatusRunning},
			after:  Task{Status: TaskStatusRunning},
			want:   false,
		},
		{
			name:   "from restarting fails",
			before: Task{Status: TaskStatusRestarting},
			after:  Task{Status: TaskStatusRestarting},
			want:   false,
		},
		{
			name:   "from cancelling fails",
			before: Task{Status: TaskStatusCancelling},
			after:  Task{Status: TaskStatusCancelling},
			want:   false,
		},
		{
			name:   "from cancelled succeeds",
			before: Task{Status: TaskStatusCancelled},
			after:  Task{Status: TaskStatusCancelled, Archived: true},
			want:   true,
		},
		{
			name:   "already archived fails",
			before: Task{Status: TaskStatusCompleted, Archived: true},
			after:  Task{Status: TaskStatusCompleted, Archived: true},
			want:   false,
		},
		{
			name:   "with command fails",
			before: Task{Status: TaskStatusCompleted, Command: TaskCommandStart},
			after:  Task{Status: TaskStatusCompleted, Command: TaskCommandStart},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.before
			assert.Equal(t, task.CanArchive(), tt.want)
			got := task.Archive()
			assert.Equal(t, got, tt.want)
			assert.DeepEqual(t, task, tt.after)
		})
	}
}

func TestTask_Cancel(t *testing.T) {
	tests := []struct {
		name   string
		before Task
		after  Task
		want   bool
	}{
		{
			name:   "from running succeeds",
			before: Task{Status: TaskStatusRunning},
			after:  Task{Status: TaskStatusCancelling, Command: TaskCommandStop, Version: 1},
			want:   true,
		},
		{
			name:   "from pending succeeds with cancelled status",
			before: Task{Status: TaskStatusPending},
			after:  Task{Status: TaskStatusCancelled},
			want:   true,
		},
		{
			name:   "from pending with command clears command",
			before: Task{Status: TaskStatusPending, Command: TaskCommandStart, Version: 1},
			after:  Task{Status: TaskStatusCancelled, Version: 1},
			want:   true,
		},
		{
			name:   "from completed fails",
			before: Task{Status: TaskStatusCompleted},
			after:  Task{Status: TaskStatusCompleted},
			want:   false,
		},
		{
			name:   "from failed fails",
			before: Task{Status: TaskStatusFailed},
			after:  Task{Status: TaskStatusFailed},
			want:   false,
		},
		{
			name:   "from restarting succeeds",
			before: Task{Status: TaskStatusRestarting},
			after:  Task{Status: TaskStatusCancelling, Command: TaskCommandStop, Version: 1},
			want:   true,
		},
		{
			name:   "from cancelling fails",
			before: Task{Status: TaskStatusCancelling},
			after:  Task{Status: TaskStatusCancelling},
			want:   false,
		},
		{
			name:   "from cancelled fails",
			before: Task{Status: TaskStatusCancelled},
			after:  Task{Status: TaskStatusCancelled},
			want:   false,
		},
		{
			name:   "archived fails",
			before: Task{Status: TaskStatusRunning, Archived: true},
			after:  Task{Status: TaskStatusRunning, Archived: true},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.before
			assert.Equal(t, task.CanCancel(), tt.want)
			got := task.Cancel()
			assert.Equal(t, got, tt.want)
			assert.DeepEqual(t, task, tt.after)
		})
	}
}

func TestTask_Restart(t *testing.T) {
	tests := []struct {
		name   string
		before Task
		after  Task
		want   bool
	}{
		{
			name:   "from running succeeds",
			before: Task{Status: TaskStatusRunning},
			after:  Task{Status: TaskStatusRestarting, Command: TaskCommandRestart, Version: 1},
			want:   true,
		},
		{
			name:   "from completed succeeds",
			before: Task{Status: TaskStatusCompleted},
			after:  Task{Status: TaskStatusPending, Command: TaskCommandRestart, Version: 1},
			want:   true,
		},
		{
			name:   "from failed succeeds",
			before: Task{Status: TaskStatusFailed},
			after:  Task{Status: TaskStatusPending, Command: TaskCommandRestart, Version: 1},
			want:   true,
		},
		{
			name:   "from pending fails",
			before: Task{Status: TaskStatusPending},
			after:  Task{Status: TaskStatusPending},
			want:   false,
		},
		{
			name:   "from restarting fails",
			before: Task{Status: TaskStatusRestarting},
			after:  Task{Status: TaskStatusRestarting},
			want:   false,
		},
		{
			name:   "from cancelling fails",
			before: Task{Status: TaskStatusCancelling},
			after:  Task{Status: TaskStatusCancelling},
			want:   false,
		},
		{
			name:   "from cancelled succeeds",
			before: Task{Status: TaskStatusCancelled},
			after:  Task{Status: TaskStatusPending, Command: TaskCommandRestart, Version: 1},
			want:   true,
		},
		{
			name:   "archived fails",
			before: Task{Status: TaskStatusCompleted, Archived: true},
			after:  Task{Status: TaskStatusCompleted, Archived: true},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.before
			assert.Equal(t, task.CanRestart(), tt.want)
			got := task.Restart()
			assert.Equal(t, got, tt.want)
			assert.DeepEqual(t, task, tt.after)
		})
	}
}

func TestTask_Start(t *testing.T) {
	tests := []struct {
		name   string
		before Task
		after  Task
		want   bool
	}{
		{
			name:   "from running succeeds without changing status",
			before: Task{Status: TaskStatusRunning},
			after:  Task{Status: TaskStatusRunning, Command: TaskCommandStart, Version: 1},
			want:   true,
		},
		{
			name:   "from completed succeeds",
			before: Task{Status: TaskStatusCompleted},
			after:  Task{Status: TaskStatusPending, Command: TaskCommandStart, Version: 1},
			want:   true,
		},
		{
			name:   "from failed succeeds",
			before: Task{Status: TaskStatusFailed},
			after:  Task{Status: TaskStatusPending, Command: TaskCommandStart, Version: 1},
			want:   true,
		},
		{
			name:   "from pending fails",
			before: Task{Status: TaskStatusPending},
			after:  Task{Status: TaskStatusPending},
			want:   false,
		},
		{
			name:   "from restarting fails",
			before: Task{Status: TaskStatusRestarting},
			after:  Task{Status: TaskStatusRestarting},
			want:   false,
		},
		{
			name:   "from cancelling fails",
			before: Task{Status: TaskStatusCancelling},
			after:  Task{Status: TaskStatusCancelling},
			want:   false,
		},
		{
			name:   "from cancelled succeeds",
			before: Task{Status: TaskStatusCancelled},
			after:  Task{Status: TaskStatusPending, Command: TaskCommandStart, Version: 1},
			want:   true,
		},
		{
			name:   "archived fails",
			before: Task{Status: TaskStatusCompleted, Archived: true},
			after:  Task{Status: TaskStatusCompleted, Archived: true},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.before
			assert.Equal(t, task.CanStart(), tt.want)
			got := task.Start()
			assert.Equal(t, got, tt.want)
			assert.DeepEqual(t, task, tt.after)
		})
	}
}
