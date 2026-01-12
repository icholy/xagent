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

func TestTask_ApplyUserAction(t *testing.T) {
	tests := []struct {
		name    string
		before  Task
		after   Task
		action  UserAction
		changed bool
	}{
		// Start action
		{
			name: "start: pending -> sets restart command",
			before: Task{
				Status:  TaskStatusPending,
				Version: 1,
			},
			after: Task{
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
				Version: 2,
			},
			action: UserAction{
				Action: UserActionStart,
			},
			changed: true,
		},
		{
			name: "start: running returns false",
			before: Task{
				Status: TaskStatusRunning,
			},
			action: UserAction{
				Action: UserActionStart,
			},
			changed: false,
		},
		{
			name: "start: completed returns false",
			before: Task{
				Status: TaskStatusCompleted,
			},
			action: UserAction{
				Action: UserActionStart,
			},
			changed: false,
		},

		// Restart action
		{
			name: "restart: running -> restarting with restart command",
			before: Task{
				Status:  TaskStatusRunning,
				Version: 1,
			},
			after: Task{
				Status:  TaskStatusRestarting,
				Command: TaskCommandRestart,
				Version: 2,
			},
			action: UserAction{
				Action: UserActionRestart,
			},
			changed: true,
		},
		{
			name: "restart: failed -> pending with restart command",
			before: Task{
				Status:  TaskStatusFailed,
				Version: 1,
			},
			after: Task{
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
				Version: 2,
			},
			action: UserAction{
				Action: UserActionRestart,
			},
			changed: true,
		},
		{
			name: "restart: completed -> pending with restart command",
			before: Task{
				Status:  TaskStatusCompleted,
				Version: 1,
			},
			after: Task{
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
				Version: 2,
			},
			action: UserAction{
				Action: UserActionRestart,
			},
			changed: true,
		},
		{
			name: "restart: cancelled -> pending with restart command",
			before: Task{
				Status:  TaskStatusCancelled,
				Version: 1,
			},
			after: Task{
				Status:  TaskStatusPending,
				Command: TaskCommandRestart,
				Version: 2,
			},
			action: UserAction{
				Action: UserActionRestart,
			},
			changed: true,
		},
		{
			name: "restart: pending returns false",
			before: Task{
				Status: TaskStatusPending,
			},
			action: UserAction{
				Action: UserActionRestart,
			},
			changed: false,
		},
		{
			name: "restart: restarting returns false",
			before: Task{
				Status: TaskStatusRestarting,
			},
			action: UserAction{
				Action: UserActionRestart,
			},
			changed: false,
		},

		// Cancel action
		{
			name: "cancel: running -> cancelling with stop command",
			before: Task{
				Status:  TaskStatusRunning,
				Version: 1,
			},
			after: Task{
				Status:  TaskStatusCancelling,
				Command: TaskCommandStop,
				Version: 2,
			},
			action: UserAction{
				Action: UserActionCancel,
			},
			changed: true,
		},
		{
			name: "cancel: restarting -> cancelling with stop command",
			before: Task{
				Status:  TaskStatusRestarting,
				Command: TaskCommandRestart,
				Version: 1,
			},
			after: Task{
				Status:  TaskStatusCancelling,
				Command: TaskCommandStop,
				Version: 2,
			},
			action: UserAction{
				Action: UserActionCancel,
			},
			changed: true,
		},
		{
			name: "cancel: pending -> cancelled",
			before: Task{
				Status:  TaskStatusPending,
				Version: 1,
			},
			after: Task{
				Status:  TaskStatusCancelled,
				Version: 2,
			},
			action: UserAction{
				Action: UserActionCancel,
			},
			changed: true,
		},
		{
			name: "cancel: completed returns false",
			before: Task{
				Status: TaskStatusCompleted,
			},
			action: UserAction{
				Action: UserActionCancel,
			},
			changed: false,
		},
		{
			name: "cancel: already cancelled returns false",
			before: Task{
				Status: TaskStatusCancelled,
			},
			action: UserAction{
				Action: UserActionCancel,
			},
			changed: false,
		},
		{
			name: "cancel: failed returns false",
			before: Task{
				Status: TaskStatusFailed,
			},
			action: UserAction{
				Action: UserActionCancel,
			},
			changed: false,
		},

		// Unknown action type
		{
			name: "unknown action type returns false",
			before: Task{
				Status: TaskStatusRunning,
			},
			action: UserAction{
				Action: UserActionType("unknown"),
			},
			changed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.before
			got := task.ApplyUserAction(&tt.action)
			assert.Equal(t, got, tt.changed)
			if tt.changed {
				assert.DeepEqual(t, task, tt.after)
			}
		})
	}
}
