package statemachine

import (
	"testing"

	"github.com/icholy/xagent/internal/model"
)

func TestUpdate_RunnerEventTransitions(t *testing.T) {
	tests := []struct {
		name           string
		task           model.Task
		event          model.RunnerEvent
		wantStatus     model.TaskStatus
		wantCommand    model.TaskCommand
		wantUpdated    bool
	}{
		// pending + restart + started -> running, clear command
		{
			name:        "pending/restart/started",
			task:        model.Task{Status: model.TaskStatusPending, Command: model.TaskCommandRestart, Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventStarted, Version: 1},
			wantStatus:  model.TaskStatusRunning,
			wantCommand: "",
			wantUpdated: true,
		},
		// pending + restart + failed -> failed, clear command
		{
			name:        "pending/restart/failed",
			task:        model.Task{Status: model.TaskStatusPending, Command: model.TaskCommandRestart, Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventFailed, Version: 1},
			wantStatus:  model.TaskStatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// restarting + restart + started -> running, clear command
		{
			name:        "restarting/restart/started",
			task:        model.Task{Status: model.TaskStatusRestarting, Command: model.TaskCommandRestart, Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventStarted, Version: 1},
			wantStatus:  model.TaskStatusRunning,
			wantCommand: "",
			wantUpdated: true,
		},
		// restarting + restart + failed -> failed, clear command
		{
			name:        "restarting/restart/failed",
			task:        model.Task{Status: model.TaskStatusRestarting, Command: model.TaskCommandRestart, Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventFailed, Version: 1},
			wantStatus:  model.TaskStatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + (no command) + stopped -> completed
		{
			name:        "running/none/stopped",
			task:        model.Task{Status: model.TaskStatusRunning, Command: "", Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventStopped, Version: 1},
			wantStatus:  model.TaskStatusCompleted,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + (no command) + failed -> failed
		{
			name:        "running/none/failed",
			task:        model.Task{Status: model.TaskStatusRunning, Command: "", Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventFailed, Version: 1},
			wantStatus:  model.TaskStatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + stop + stopped -> cancelled, clear command
		{
			name:        "running/stop/stopped",
			task:        model.Task{Status: model.TaskStatusRunning, Command: model.TaskCommandStop, Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventStopped, Version: 1},
			wantStatus:  model.TaskStatusCancelled,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + stop + failed -> failed, clear command
		{
			name:        "running/stop/failed",
			task:        model.Task{Status: model.TaskStatusRunning, Command: model.TaskCommandStop, Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventFailed, Version: 1},
			wantStatus:  model.TaskStatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + restart + started -> running, clear command
		{
			name:        "running/restart/started",
			task:        model.Task{Status: model.TaskStatusRunning, Command: model.TaskCommandRestart, Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventStarted, Version: 1},
			wantStatus:  model.TaskStatusRunning,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + restart + failed -> failed, clear command
		{
			name:        "running/restart/failed",
			task:        model.Task{Status: model.TaskStatusRunning, Command: model.TaskCommandRestart, Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventFailed, Version: 1},
			wantStatus:  model.TaskStatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// cancelling + stop + stopped -> cancelled, clear command
		{
			name:        "cancelling/stop/stopped",
			task:        model.Task{Status: model.TaskStatusCancelling, Command: model.TaskCommandStop, Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventStopped, Version: 1},
			wantStatus:  model.TaskStatusCancelled,
			wantCommand: "",
			wantUpdated: true,
		},
		// cancelling + stop + failed -> failed, clear command
		{
			name:        "cancelling/stop/failed",
			task:        model.Task{Status: model.TaskStatusCancelling, Command: model.TaskCommandStop, Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventFailed, Version: 1},
			wantStatus:  model.TaskStatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.task
			event := tt.event
			got := Update(&task, &event)
			if got != tt.wantUpdated {
				t.Errorf("Update() returned %v, want %v", got, tt.wantUpdated)
			}
			if task.Status != tt.wantStatus {
				t.Errorf("task.Status = %q, want %q", task.Status, tt.wantStatus)
			}
			if task.Command != tt.wantCommand {
				t.Errorf("task.Command = %q, want %q", task.Command, tt.wantCommand)
			}
		})
	}
}

func TestUpdate_VersionMismatch(t *testing.T) {
	// Event with non-zero version that doesn't match task version should be ignored
	task := model.Task{Status: model.TaskStatusPending, Command: model.TaskCommandRestart, Version: 2}
	event := model.RunnerEvent{Event: model.RunnerEventStarted, Version: 1}

	updated := Update(&task, &event)
	if updated {
		t.Error("Update() should return false for version mismatch")
	}
	if task.Status != model.TaskStatusPending {
		t.Errorf("task.Status = %q, want %q", task.Status, model.TaskStatusPending)
	}
	if task.Command != model.TaskCommandRestart {
		t.Errorf("task.Command = %q, want %q", task.Command, model.TaskCommandRestart)
	}
}

func TestUpdate_VersionBypass(t *testing.T) {
	// Event with version 0 should bypass version check (for spontaneous failures)
	task := model.Task{Status: model.TaskStatusRunning, Command: "", Version: 5}
	event := model.RunnerEvent{Event: model.RunnerEventFailed, Version: 0}

	updated := Update(&task, &event)
	if !updated {
		t.Error("Update() should return true for version 0 bypass")
	}
	if task.Status != model.TaskStatusFailed {
		t.Errorf("task.Status = %q, want %q", task.Status, model.TaskStatusFailed)
	}
}

func TestUpdate_ReconcileEvents(t *testing.T) {
	tests := []struct {
		name        string
		task        model.Task
		event       model.RunnerEvent
		wantStatus  model.TaskStatus
		wantCommand model.TaskCommand
		wantUpdated bool
	}{
		// Reconcile started on running task should not change anything
		{
			name:        "reconcile_started_running_no_change",
			task:        model.Task{Status: model.TaskStatusRunning, Command: "", Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventStarted, Version: 1, Reconcile: true},
			wantStatus:  model.TaskStatusRunning,
			wantCommand: "",
			wantUpdated: false,
		},
		// Reconcile started on pending task with restart should update to running
		{
			name:        "reconcile_started_pending",
			task:        model.Task{Status: model.TaskStatusPending, Command: model.TaskCommandRestart, Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventStarted, Version: 1, Reconcile: true},
			wantStatus:  model.TaskStatusRunning,
			wantCommand: "",
			wantUpdated: true,
		},
		// Reconcile stopped on running task should complete
		{
			name:        "reconcile_stopped_running",
			task:        model.Task{Status: model.TaskStatusRunning, Command: "", Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventStopped, Version: 1, Reconcile: true},
			wantStatus:  model.TaskStatusCompleted,
			wantCommand: "",
			wantUpdated: true,
		},
		// Reconcile stopped on already completed task should not change
		{
			name:        "reconcile_stopped_completed_no_change",
			task:        model.Task{Status: model.TaskStatusCompleted, Command: "", Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventStopped, Version: 1, Reconcile: true},
			wantStatus:  model.TaskStatusCompleted,
			wantCommand: "",
			wantUpdated: false,
		},
		// Reconcile failed on running task should fail
		{
			name:        "reconcile_failed_running",
			task:        model.Task{Status: model.TaskStatusRunning, Command: "", Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventFailed, Version: 1, Reconcile: true},
			wantStatus:  model.TaskStatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// Reconcile failed on already failed task should not change
		{
			name:        "reconcile_failed_already_failed",
			task:        model.Task{Status: model.TaskStatusFailed, Command: "", Version: 1},
			event:       model.RunnerEvent{Event: model.RunnerEventFailed, Version: 1, Reconcile: true},
			wantStatus:  model.TaskStatusFailed,
			wantCommand: "",
			wantUpdated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.task
			event := tt.event
			got := Update(&task, &event)
			if got != tt.wantUpdated {
				t.Errorf("Update() returned %v, want %v", got, tt.wantUpdated)
			}
			if task.Status != tt.wantStatus {
				t.Errorf("task.Status = %q, want %q", task.Status, tt.wantStatus)
			}
			if task.Command != tt.wantCommand {
				t.Errorf("task.Command = %q, want %q", task.Command, tt.wantCommand)
			}
		})
	}
}

func TestUpdate_NoChange(t *testing.T) {
	// Events that don't match any transition rule should not modify the task
	tests := []struct {
		name  string
		task  model.Task
		event model.RunnerEvent
	}{
		{
			name:  "completed_task_ignores_events",
			task:  model.Task{Status: model.TaskStatusCompleted, Command: "", Version: 1},
			event: model.RunnerEvent{Event: model.RunnerEventStarted, Version: 1},
		},
		{
			name:  "cancelled_task_ignores_events",
			task:  model.Task{Status: model.TaskStatusCancelled, Command: "", Version: 1},
			event: model.RunnerEvent{Event: model.RunnerEventStarted, Version: 1},
		},
		{
			name:  "pending_without_restart_command",
			task:  model.Task{Status: model.TaskStatusPending, Command: "", Version: 1},
			event: model.RunnerEvent{Event: model.RunnerEventStarted, Version: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalStatus := tt.task.Status
			originalCommand := tt.task.Command
			task := tt.task
			event := tt.event
			got := Update(&task, &event)
			if got {
				t.Error("Update() should return false when no change")
			}
			if task.Status != originalStatus {
				t.Errorf("task.Status changed from %q to %q", originalStatus, task.Status)
			}
			if task.Command != originalCommand {
				t.Errorf("task.Command changed from %q to %q", originalCommand, task.Command)
			}
		})
	}
}

func TestUpdate_FailedAlwaysResultsInFailed(t *testing.T) {
	// Key rule: A failed event always results in failed status
	statuses := []model.TaskStatus{model.TaskStatusPending, model.TaskStatusRestarting, model.TaskStatusRunning, model.TaskStatusCancelling}
	commands := []model.TaskCommand{"", model.TaskCommandRestart, model.TaskCommandStop}

	for _, status := range statuses {
		for _, command := range commands {
			t.Run(string(status)+"/"+string(command), func(t *testing.T) {
				task := model.Task{Status: status, Command: command, Version: 1}
				event := model.RunnerEvent{Event: model.RunnerEventFailed, Version: 1}
				Update(&task, &event)
				if task.Status != model.TaskStatusFailed {
					t.Errorf("after failed event: task.Status = %q, want %q", task.Status, model.TaskStatusFailed)
				}
			})
		}
	}
}
