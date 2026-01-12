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
		before TaskStatus
		want   bool
		after  TaskStatus
	}{
		{
			name:   "from completed succeeds",
			before: TaskStatusCompleted,
			want:   true,
			after:  TaskStatusArchived,
		},
		{
			name:   "from failed succeeds",
			before: TaskStatusFailed,
			want:   true,
			after:  TaskStatusArchived,
		},
		{
			name:   "from pending fails",
			before: TaskStatusPending,
			want:   false,
			after:  TaskStatusPending,
		},
		{
			name:   "from running fails",
			before: TaskStatusRunning,
			want:   false,
			after:  TaskStatusRunning,
		},
		{
			name:   "from restarting fails",
			before: TaskStatusRestarting,
			want:   false,
			after:  TaskStatusRestarting,
		},
		{
			name:   "from cancelling fails",
			before: TaskStatusCancelling,
			want:   false,
			after:  TaskStatusCancelling,
		},
		{
			name:   "from cancelled fails",
			before: TaskStatusCancelled,
			want:   false,
			after:  TaskStatusCancelled,
		},
		{
			name:   "from archived fails",
			before: TaskStatusArchived,
			want:   false,
			after:  TaskStatusArchived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Status: tt.before}
			got := task.Archive()
			assert.Equal(t, got, tt.want)
			assert.Equal(t, task.Status, tt.after)
		})
	}
}

func TestTask_Cancel(t *testing.T) {
	tests := []struct {
		name   string
		before TaskStatus
		want   bool
		after  TaskStatus
	}{
		{
			name:   "from running succeeds",
			before: TaskStatusRunning,
			want:   true,
			after:  TaskStatusCancelling,
		},
		{
			name:   "from pending succeeds",
			before: TaskStatusPending,
			want:   true,
			after:  TaskStatusCancelling,
		},
		{
			name:   "from completed fails",
			before: TaskStatusCompleted,
			want:   false,
			after:  TaskStatusCompleted,
		},
		{
			name:   "from failed fails",
			before: TaskStatusFailed,
			want:   false,
			after:  TaskStatusFailed,
		},
		{
			name:   "from restarting fails",
			before: TaskStatusRestarting,
			want:   false,
			after:  TaskStatusRestarting,
		},
		{
			name:   "from cancelling fails",
			before: TaskStatusCancelling,
			want:   false,
			after:  TaskStatusCancelling,
		},
		{
			name:   "from cancelled fails",
			before: TaskStatusCancelled,
			want:   false,
			after:  TaskStatusCancelled,
		},
		{
			name:   "from archived fails",
			before: TaskStatusArchived,
			want:   false,
			after:  TaskStatusArchived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Status: tt.before}
			got := task.Cancel()
			assert.Equal(t, got, tt.want)
			assert.Equal(t, task.Status, tt.after)
		})
	}
}

func TestTask_Restart(t *testing.T) {
	tests := []struct {
		name   string
		before TaskStatus
		want   bool
		after  TaskStatus
	}{
		{
			name:   "from running succeeds",
			before: TaskStatusRunning,
			want:   true,
			after:  TaskStatusRestarting,
		},
		{
			name:   "from completed succeeds",
			before: TaskStatusCompleted,
			want:   true,
			after:  TaskStatusRestarting,
		},
		{
			name:   "from failed succeeds",
			before: TaskStatusFailed,
			want:   true,
			after:  TaskStatusRestarting,
		},
		{
			name:   "from pending fails",
			before: TaskStatusPending,
			want:   false,
			after:  TaskStatusPending,
		},
		{
			name:   "from restarting fails",
			before: TaskStatusRestarting,
			want:   false,
			after:  TaskStatusRestarting,
		},
		{
			name:   "from cancelling fails",
			before: TaskStatusCancelling,
			want:   false,
			after:  TaskStatusCancelling,
		},
		{
			name:   "from cancelled fails",
			before: TaskStatusCancelled,
			want:   false,
			after:  TaskStatusCancelled,
		},
		{
			name:   "from archived fails",
			before: TaskStatusArchived,
			want:   false,
			after:  TaskStatusArchived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Status: tt.before}
			got := task.Restart()
			assert.Equal(t, got, tt.want)
			assert.Equal(t, task.Status, tt.after)
		})
	}
}
