package model

import (
	"testing"

	"gotest.tools/v3/assert"
)

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
			after:  Task{Status: TaskStatusArchived},
			want:   true,
		},
		{
			name:   "from failed succeeds",
			before: Task{Status: TaskStatusFailed},
			after:  Task{Status: TaskStatusArchived},
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
			after:  Task{Status: TaskStatusArchived},
			want:   true,
		},
		{
			name:   "from archived fails",
			before: Task{Status: TaskStatusArchived},
			after:  Task{Status: TaskStatusArchived},
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
			name:   "from archived fails",
			before: Task{Status: TaskStatusArchived},
			after:  Task{Status: TaskStatusArchived},
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
			name:   "from archived fails",
			before: Task{Status: TaskStatusArchived},
			after:  Task{Status: TaskStatusArchived},
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
			name:   "from archived fails",
			before: Task{Status: TaskStatusArchived},
			after:  Task{Status: TaskStatusArchived},
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
