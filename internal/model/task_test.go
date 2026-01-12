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

func TestTask_SetStatus(t *testing.T) {
	tests := []struct {
		name      string
		before    TaskStatus
		newStatus TaskStatus
		want      bool
		after     TaskStatus
	}{
		// Archive transitions (only from completed or failed)
		{
			name:      "archive from completed succeeds",
			before:    TaskStatusCompleted,
			newStatus: TaskStatusArchived,
			want:      true,
			after:     TaskStatusArchived,
		},
		{
			name:      "archive from failed succeeds",
			before:    TaskStatusFailed,
			newStatus: TaskStatusArchived,
			want:      true,
			after:     TaskStatusArchived,
		},
		{
			name:      "archive from pending fails",
			before:    TaskStatusPending,
			newStatus: TaskStatusArchived,
			want:      false,
			after:     TaskStatusPending,
		},
		{
			name:      "archive from running fails",
			before:    TaskStatusRunning,
			newStatus: TaskStatusArchived,
			want:      false,
			after:     TaskStatusRunning,
		},
		{
			name:      "archive from restarting fails",
			before:    TaskStatusRestarting,
			newStatus: TaskStatusArchived,
			want:      false,
			after:     TaskStatusRestarting,
		},
		{
			name:      "archive from cancelling fails",
			before:    TaskStatusCancelling,
			newStatus: TaskStatusArchived,
			want:      false,
			after:     TaskStatusCancelling,
		},
		{
			name:      "archive from cancelled fails",
			before:    TaskStatusCancelled,
			newStatus: TaskStatusArchived,
			want:      false,
			after:     TaskStatusCancelled,
		},
		{
			name:      "archive from archived fails",
			before:    TaskStatusArchived,
			newStatus: TaskStatusArchived,
			want:      false,
			after:     TaskStatusArchived,
		},

		// Cancel transitions (only from running or pending)
		{
			name:      "cancel from running succeeds",
			before:    TaskStatusRunning,
			newStatus: TaskStatusCancelling,
			want:      true,
			after:     TaskStatusCancelling,
		},
		{
			name:      "cancel from pending succeeds",
			before:    TaskStatusPending,
			newStatus: TaskStatusCancelling,
			want:      true,
			after:     TaskStatusCancelling,
		},
		{
			name:      "cancel from completed fails",
			before:    TaskStatusCompleted,
			newStatus: TaskStatusCancelling,
			want:      false,
			after:     TaskStatusCompleted,
		},
		{
			name:      "cancel from failed fails",
			before:    TaskStatusFailed,
			newStatus: TaskStatusCancelling,
			want:      false,
			after:     TaskStatusFailed,
		},
		{
			name:      "cancel from restarting fails",
			before:    TaskStatusRestarting,
			newStatus: TaskStatusCancelling,
			want:      false,
			after:     TaskStatusRestarting,
		},
		{
			name:      "cancel from cancelling fails",
			before:    TaskStatusCancelling,
			newStatus: TaskStatusCancelling,
			want:      false,
			after:     TaskStatusCancelling,
		},
		{
			name:      "cancel from cancelled fails",
			before:    TaskStatusCancelled,
			newStatus: TaskStatusCancelling,
			want:      false,
			after:     TaskStatusCancelled,
		},
		{
			name:      "cancel from archived fails",
			before:    TaskStatusArchived,
			newStatus: TaskStatusCancelling,
			want:      false,
			after:     TaskStatusArchived,
		},

		// Restart transitions (only from running, completed, or failed)
		{
			name:      "restart from running succeeds",
			before:    TaskStatusRunning,
			newStatus: TaskStatusRestarting,
			want:      true,
			after:     TaskStatusRestarting,
		},
		{
			name:      "restart from completed succeeds",
			before:    TaskStatusCompleted,
			newStatus: TaskStatusRestarting,
			want:      true,
			after:     TaskStatusRestarting,
		},
		{
			name:      "restart from failed succeeds",
			before:    TaskStatusFailed,
			newStatus: TaskStatusRestarting,
			want:      true,
			after:     TaskStatusRestarting,
		},
		{
			name:      "restart from pending fails",
			before:    TaskStatusPending,
			newStatus: TaskStatusRestarting,
			want:      false,
			after:     TaskStatusPending,
		},
		{
			name:      "restart from restarting fails",
			before:    TaskStatusRestarting,
			newStatus: TaskStatusRestarting,
			want:      false,
			after:     TaskStatusRestarting,
		},
		{
			name:      "restart from cancelling fails",
			before:    TaskStatusCancelling,
			newStatus: TaskStatusRestarting,
			want:      false,
			after:     TaskStatusCancelling,
		},
		{
			name:      "restart from cancelled fails",
			before:    TaskStatusCancelled,
			newStatus: TaskStatusRestarting,
			want:      false,
			after:     TaskStatusCancelled,
		},
		{
			name:      "restart from archived fails",
			before:    TaskStatusArchived,
			newStatus: TaskStatusRestarting,
			want:      false,
			after:     TaskStatusArchived,
		},

		// Unsupported target status values
		{
			name:      "setting to running fails",
			before:    TaskStatusPending,
			newStatus: TaskStatusRunning,
			want:      false,
			after:     TaskStatusPending,
		},
		{
			name:      "setting to completed fails",
			before:    TaskStatusRunning,
			newStatus: TaskStatusCompleted,
			want:      false,
			after:     TaskStatusRunning,
		},
		{
			name:      "setting to failed fails",
			before:    TaskStatusRunning,
			newStatus: TaskStatusFailed,
			want:      false,
			after:     TaskStatusRunning,
		},
		{
			name:      "setting to cancelled fails",
			before:    TaskStatusCancelling,
			newStatus: TaskStatusCancelled,
			want:      false,
			after:     TaskStatusCancelling,
		},
		{
			name:      "setting to pending fails",
			before:    TaskStatusRunning,
			newStatus: TaskStatusPending,
			want:      false,
			after:     TaskStatusRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Status: tt.before}
			got := task.SetStatus(tt.newStatus)
			assert.Equal(t, got, tt.want)
			assert.Equal(t, task.Status, tt.after)
		})
	}
}
