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
		// Reconcile events
		{
			name: "reconcile event returns false",
			before: Task{
				ID:      1,
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:    1,
				Event:     RunnerEventStarted,
				Version:   1,
				Reconcile: true,
			},
			changed: false,
		},

		// Version mismatch
		{
			name: "version mismatch returns false",
			before: Task{
				ID:      1,
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
				Version: 2,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
				Version: 2,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStarted,
				Version: 1,
			},
			changed: false,
		},
		{
			name: "version 0 bypasses check",
			before: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: "",
				Version: 5,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusFailed,
				Command: "",
				Version: 5,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventFailed,
				Version: 0,
			},
			changed: true,
		},

		// Started events
		{
			name: "started: pending with restart -> running",
			before: Task{
				ID:      1,
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStarted,
				Version: 1,
			},
			changed: true,
		},
		{
			name: "started: restarting with restart -> running",
			before: Task{
				ID:      1,
				Status:  TaskStatusRestarting,
				Command: TaskCommandRestart,
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStarted,
				Version: 1,
			},
			changed: true,
		},
		{
			name: "started: running with restart -> running (SIGHUP case)",
			before: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: TaskCommandRestart,
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStarted,
				Version: 1,
			},
			changed: true,
		},
		{
			name: "started: pending without restart command returns false",
			before: Task{
				ID:      1,
				Status:  TaskStatusPending,
				Command: "",
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusPending,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStarted,
				Version: 1,
			},
			changed: false,
		},
		{
			name: "started: completed status returns false",
			before: Task{
				ID:      1,
				Status:  TaskStatusCompleted,
				Command: "",
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusCompleted,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStarted,
				Version: 1,
			},
			changed: false,
		},

		// Stopped events
		{
			name: "stopped: running with no command -> completed",
			before: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: "",
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusCompleted,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStopped,
				Version: 1,
			},
			changed: true,
		},
		{
			name: "stopped: running with stop command -> cancelled",
			before: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: TaskCommandStop,
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusCancelled,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStopped,
				Version: 1,
			},
			changed: true,
		},
		{
			name: "stopped: cancelling with stop command -> cancelled",
			before: Task{
				ID:      1,
				Status:  TaskStatusCancelling,
				Command: TaskCommandStop,
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusCancelled,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStopped,
				Version: 1,
			},
			changed: true,
		},
		{
			name: "stopped: running with restart command returns false",
			before: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: TaskCommandRestart,
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: TaskCommandRestart,
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStopped,
				Version: 1,
			},
			changed: false,
		},
		{
			name: "stopped: pending status returns false",
			before: Task{
				ID:      1,
				Status:  TaskStatusPending,
				Command: "",
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusPending,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStopped,
				Version: 1,
			},
			changed: false,
		},

		// Failed events
		{
			name: "failed: pending -> failed",
			before: Task{
				ID:      1,
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusFailed,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventFailed,
				Version: 1,
			},
			changed: true,
		},
		{
			name: "failed: restarting -> failed",
			before: Task{
				ID:      1,
				Status:  TaskStatusRestarting,
				Command: TaskCommandRestart,
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusFailed,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventFailed,
				Version: 1,
			},
			changed: true,
		},
		{
			name: "failed: running -> failed",
			before: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: "",
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusFailed,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventFailed,
				Version: 1,
			},
			changed: true,
		},
		{
			name: "failed: running with stop command -> failed",
			before: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: TaskCommandStop,
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusFailed,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventFailed,
				Version: 1,
			},
			changed: true,
		},
		{
			name: "failed: cancelling -> failed",
			before: Task{
				ID:      1,
				Status:  TaskStatusCancelling,
				Command: TaskCommandStop,
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusFailed,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventFailed,
				Version: 1,
			},
			changed: true,
		},
		{
			name: "failed: already failed returns false",
			before: Task{
				ID:      1,
				Status:  TaskStatusFailed,
				Command: "",
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusFailed,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventFailed,
				Version: 1,
			},
			changed: false,
		},
		{
			name: "failed: completed returns false",
			before: Task{
				ID:      1,
				Status:  TaskStatusCompleted,
				Command: "",
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusCompleted,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventFailed,
				Version: 1,
			},
			changed: false,
		},
		{
			name: "failed: cancelled returns false",
			before: Task{
				ID:      1,
				Status:  TaskStatusCancelled,
				Command: "",
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusCancelled,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventFailed,
				Version: 1,
			},
			changed: false,
		},

		// Unknown event type
		{
			name: "unknown event type returns false",
			before: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: "",
				Version: 1,
			},
			after: Task{
				ID:      1,
				Status:  TaskStatusRunning,
				Command: "",
				Version: 1,
			},
			event: RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventType("unknown"),
				Version: 1,
			},
			changed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.before
			got := task.ApplyRunnerEvent(&tt.event)
			assert.Equal(t, got, tt.changed)
			assert.DeepEqual(t, task, tt.after)
		})
	}
}
