package statemachine

import "testing"

func TestUpdate_RunnerEventTransitions(t *testing.T) {
	tests := []struct {
		name           string
		task           Task
		event          RunnerEvent
		wantStatus     string
		wantCommand    string
		wantUpdated    bool
	}{
		// pending + restart + started -> running, clear command
		{
			name:        "pending/restart/started",
			task:        Task{Status: StatusPending, Command: CommandRestart, Version: 1},
			event:       RunnerEvent{Event: EventStarted, Version: 1},
			wantStatus:  StatusRunning,
			wantCommand: "",
			wantUpdated: true,
		},
		// pending + restart + failed -> failed, clear command
		{
			name:        "pending/restart/failed",
			task:        Task{Status: StatusPending, Command: CommandRestart, Version: 1},
			event:       RunnerEvent{Event: EventFailed, Version: 1},
			wantStatus:  StatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// restarting + restart + started -> running, clear command
		{
			name:        "restarting/restart/started",
			task:        Task{Status: StatusRestarting, Command: CommandRestart, Version: 1},
			event:       RunnerEvent{Event: EventStarted, Version: 1},
			wantStatus:  StatusRunning,
			wantCommand: "",
			wantUpdated: true,
		},
		// restarting + restart + failed -> failed, clear command
		{
			name:        "restarting/restart/failed",
			task:        Task{Status: StatusRestarting, Command: CommandRestart, Version: 1},
			event:       RunnerEvent{Event: EventFailed, Version: 1},
			wantStatus:  StatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + (no command) + stopped -> completed
		{
			name:        "running/none/stopped",
			task:        Task{Status: StatusRunning, Command: "", Version: 1},
			event:       RunnerEvent{Event: EventStopped, Version: 1},
			wantStatus:  StatusCompleted,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + (no command) + failed -> failed
		{
			name:        "running/none/failed",
			task:        Task{Status: StatusRunning, Command: "", Version: 1},
			event:       RunnerEvent{Event: EventFailed, Version: 1},
			wantStatus:  StatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + stop + stopped -> cancelled, clear command
		{
			name:        "running/stop/stopped",
			task:        Task{Status: StatusRunning, Command: CommandStop, Version: 1},
			event:       RunnerEvent{Event: EventStopped, Version: 1},
			wantStatus:  StatusCancelled,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + stop + failed -> failed, clear command
		{
			name:        "running/stop/failed",
			task:        Task{Status: StatusRunning, Command: CommandStop, Version: 1},
			event:       RunnerEvent{Event: EventFailed, Version: 1},
			wantStatus:  StatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + restart + started -> running, clear command
		{
			name:        "running/restart/started",
			task:        Task{Status: StatusRunning, Command: CommandRestart, Version: 1},
			event:       RunnerEvent{Event: EventStarted, Version: 1},
			wantStatus:  StatusRunning,
			wantCommand: "",
			wantUpdated: true,
		},
		// running + restart + failed -> failed, clear command
		{
			name:        "running/restart/failed",
			task:        Task{Status: StatusRunning, Command: CommandRestart, Version: 1},
			event:       RunnerEvent{Event: EventFailed, Version: 1},
			wantStatus:  StatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// cancelling + stop + stopped -> cancelled, clear command
		{
			name:        "cancelling/stop/stopped",
			task:        Task{Status: StatusCancelling, Command: CommandStop, Version: 1},
			event:       RunnerEvent{Event: EventStopped, Version: 1},
			wantStatus:  StatusCancelled,
			wantCommand: "",
			wantUpdated: true,
		},
		// cancelling + stop + failed -> failed, clear command
		{
			name:        "cancelling/stop/failed",
			task:        Task{Status: StatusCancelling, Command: CommandStop, Version: 1},
			event:       RunnerEvent{Event: EventFailed, Version: 1},
			wantStatus:  StatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.task
			got := Update(&task, tt.event)
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
	task := Task{Status: StatusPending, Command: CommandRestart, Version: 2}
	event := RunnerEvent{Event: EventStarted, Version: 1}

	updated := Update(&task, event)
	if updated {
		t.Error("Update() should return false for version mismatch")
	}
	if task.Status != StatusPending {
		t.Errorf("task.Status = %q, want %q", task.Status, StatusPending)
	}
	if task.Command != CommandRestart {
		t.Errorf("task.Command = %q, want %q", task.Command, CommandRestart)
	}
}

func TestUpdate_VersionBypass(t *testing.T) {
	// Event with version 0 should bypass version check (for spontaneous failures)
	task := Task{Status: StatusRunning, Command: "", Version: 5}
	event := RunnerEvent{Event: EventFailed, Version: 0}

	updated := Update(&task, event)
	if !updated {
		t.Error("Update() should return true for version 0 bypass")
	}
	if task.Status != StatusFailed {
		t.Errorf("task.Status = %q, want %q", task.Status, StatusFailed)
	}
}

func TestUpdate_ReconcileEvents(t *testing.T) {
	tests := []struct {
		name        string
		task        Task
		event       RunnerEvent
		wantStatus  string
		wantCommand string
		wantUpdated bool
	}{
		// Reconcile started on running task should not change anything
		{
			name:        "reconcile_started_running_no_change",
			task:        Task{Status: StatusRunning, Command: "", Version: 1},
			event:       RunnerEvent{Event: EventStarted, Version: 1, Reconcile: true},
			wantStatus:  StatusRunning,
			wantCommand: "",
			wantUpdated: false,
		},
		// Reconcile started on pending task with restart should update to running
		{
			name:        "reconcile_started_pending",
			task:        Task{Status: StatusPending, Command: CommandRestart, Version: 1},
			event:       RunnerEvent{Event: EventStarted, Version: 1, Reconcile: true},
			wantStatus:  StatusRunning,
			wantCommand: "",
			wantUpdated: true,
		},
		// Reconcile stopped on running task should complete
		{
			name:        "reconcile_stopped_running",
			task:        Task{Status: StatusRunning, Command: "", Version: 1},
			event:       RunnerEvent{Event: EventStopped, Version: 1, Reconcile: true},
			wantStatus:  StatusCompleted,
			wantCommand: "",
			wantUpdated: true,
		},
		// Reconcile stopped on already completed task should not change
		{
			name:        "reconcile_stopped_completed_no_change",
			task:        Task{Status: StatusCompleted, Command: "", Version: 1},
			event:       RunnerEvent{Event: EventStopped, Version: 1, Reconcile: true},
			wantStatus:  StatusCompleted,
			wantCommand: "",
			wantUpdated: false,
		},
		// Reconcile failed on running task should fail
		{
			name:        "reconcile_failed_running",
			task:        Task{Status: StatusRunning, Command: "", Version: 1},
			event:       RunnerEvent{Event: EventFailed, Version: 1, Reconcile: true},
			wantStatus:  StatusFailed,
			wantCommand: "",
			wantUpdated: true,
		},
		// Reconcile failed on already failed task should not change
		{
			name:        "reconcile_failed_already_failed",
			task:        Task{Status: StatusFailed, Command: "", Version: 1},
			event:       RunnerEvent{Event: EventFailed, Version: 1, Reconcile: true},
			wantStatus:  StatusFailed,
			wantCommand: "",
			wantUpdated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.task
			got := Update(&task, tt.event)
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
		task  Task
		event RunnerEvent
	}{
		{
			name:  "completed_task_ignores_events",
			task:  Task{Status: StatusCompleted, Command: "", Version: 1},
			event: RunnerEvent{Event: EventStarted, Version: 1},
		},
		{
			name:  "cancelled_task_ignores_events",
			task:  Task{Status: StatusCancelled, Command: "", Version: 1},
			event: RunnerEvent{Event: EventStarted, Version: 1},
		},
		{
			name:  "pending_without_restart_command",
			task:  Task{Status: StatusPending, Command: "", Version: 1},
			event: RunnerEvent{Event: EventStarted, Version: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalStatus := tt.task.Status
			originalCommand := tt.task.Command
			task := tt.task
			got := Update(&task, tt.event)
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
	statuses := []string{StatusPending, StatusRestarting, StatusRunning, StatusCancelling}
	commands := []string{"", CommandRestart, CommandStop}

	for _, status := range statuses {
		for _, command := range commands {
			t.Run(status+"/"+command, func(t *testing.T) {
				task := Task{Status: status, Command: command, Version: 1}
				event := RunnerEvent{Event: EventFailed, Version: 1}
				Update(&task, event)
				if task.Status != StatusFailed {
					t.Errorf("after failed event: task.Status = %q, want %q", task.Status, StatusFailed)
				}
			})
		}
	}
}
