package model

import "testing"

func TestTask_ApplyRunnerEvent_Reconcile(t *testing.T) {
	task := &Task{
		ID:      1,
		Status:  TaskStatusPending,
		Command: TaskCommandRestart,
		Version: 1,
	}

	event := &RunnerEvent{
		TaskID:    1,
		Event:     RunnerEventStarted,
		Version:   1,
		Reconcile: true,
	}

	if task.ApplyRunnerEvent(event) {
		t.Error("expected ApplyRunnerEvent to return false for reconcile events")
	}

	if task.Status != TaskStatusPending {
		t.Errorf("expected status to remain pending, got %s", task.Status)
	}
}

func TestTask_ApplyRunnerEvent_VersionMismatch(t *testing.T) {
	task := &Task{
		ID:      1,
		Status:  TaskStatusPending,
		Command: TaskCommandRestart,
		Version: 2,
	}

	event := &RunnerEvent{
		TaskID:  1,
		Event:   RunnerEventStarted,
		Version: 1, // mismatched version
	}

	if task.ApplyRunnerEvent(event) {
		t.Error("expected ApplyRunnerEvent to return false for version mismatch")
	}

	if task.Status != TaskStatusPending {
		t.Errorf("expected status to remain pending, got %s", task.Status)
	}
}

func TestTask_ApplyRunnerEvent_VersionZeroBypass(t *testing.T) {
	task := &Task{
		ID:      1,
		Status:  TaskStatusRunning,
		Command: "",
		Version: 5,
	}

	event := &RunnerEvent{
		TaskID:  1,
		Event:   RunnerEventFailed,
		Version: 0, // version 0 bypasses check
	}

	if !task.ApplyRunnerEvent(event) {
		t.Error("expected ApplyRunnerEvent to return true for version 0 bypass")
	}

	if task.Status != TaskStatusFailed {
		t.Errorf("expected status to be failed, got %s", task.Status)
	}
}

func TestTask_ApplyRunnerEvent_Started(t *testing.T) {
	tests := []struct {
		name           string
		initialStatus  TaskStatus
		initialCommand TaskCommand
		version        int64
		wantUpdated    bool
		wantStatus     TaskStatus
		wantCommand    TaskCommand
	}{
		{
			name:           "pending with restart command",
			initialStatus:  TaskStatusPending,
			initialCommand: TaskCommandRestart,
			version:        1,
			wantUpdated:    true,
			wantStatus:     TaskStatusRunning,
			wantCommand:    "",
		},
		{
			name:           "restarting with restart command",
			initialStatus:  TaskStatusRestarting,
			initialCommand: TaskCommandRestart,
			version:        1,
			wantUpdated:    true,
			wantStatus:     TaskStatusRunning,
			wantCommand:    "",
		},
		{
			name:           "running with restart command (SIGHUP case)",
			initialStatus:  TaskStatusRunning,
			initialCommand: TaskCommandRestart,
			version:        1,
			wantUpdated:    true,
			wantStatus:     TaskStatusRunning,
			wantCommand:    "",
		},
		{
			name:           "pending without restart command",
			initialStatus:  TaskStatusPending,
			initialCommand: "",
			version:        1,
			wantUpdated:    false,
			wantStatus:     TaskStatusPending,
			wantCommand:    "",
		},
		{
			name:           "completed status",
			initialStatus:  TaskStatusCompleted,
			initialCommand: "",
			version:        1,
			wantUpdated:    false,
			wantStatus:     TaskStatusCompleted,
			wantCommand:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{
				ID:      1,
				Status:  tt.initialStatus,
				Command: tt.initialCommand,
				Version: tt.version,
			}

			event := &RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStarted,
				Version: tt.version,
			}

			got := task.ApplyRunnerEvent(event)
			if got != tt.wantUpdated {
				t.Errorf("ApplyRunnerEvent() = %v, want %v", got, tt.wantUpdated)
			}

			if task.Status != tt.wantStatus {
				t.Errorf("Status = %s, want %s", task.Status, tt.wantStatus)
			}

			if task.Command != tt.wantCommand {
				t.Errorf("Command = %s, want %s", task.Command, tt.wantCommand)
			}
		})
	}
}

func TestTask_ApplyRunnerEvent_Stopped(t *testing.T) {
	tests := []struct {
		name           string
		initialStatus  TaskStatus
		initialCommand TaskCommand
		version        int64
		wantUpdated    bool
		wantStatus     TaskStatus
		wantCommand    TaskCommand
	}{
		{
			name:           "running with no command (clean exit)",
			initialStatus:  TaskStatusRunning,
			initialCommand: "",
			version:        1,
			wantUpdated:    true,
			wantStatus:     TaskStatusCompleted,
			wantCommand:    "",
		},
		{
			name:           "running with stop command",
			initialStatus:  TaskStatusRunning,
			initialCommand: TaskCommandStop,
			version:        1,
			wantUpdated:    true,
			wantStatus:     TaskStatusCancelled,
			wantCommand:    "",
		},
		{
			name:           "cancelling with stop command",
			initialStatus:  TaskStatusCancelling,
			initialCommand: TaskCommandStop,
			version:        1,
			wantUpdated:    true,
			wantStatus:     TaskStatusCancelled,
			wantCommand:    "",
		},
		{
			name:           "running with restart command (unexpected)",
			initialStatus:  TaskStatusRunning,
			initialCommand: TaskCommandRestart,
			version:        1,
			wantUpdated:    false,
			wantStatus:     TaskStatusRunning,
			wantCommand:    TaskCommandRestart,
		},
		{
			name:           "pending status (unexpected)",
			initialStatus:  TaskStatusPending,
			initialCommand: "",
			version:        1,
			wantUpdated:    false,
			wantStatus:     TaskStatusPending,
			wantCommand:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{
				ID:      1,
				Status:  tt.initialStatus,
				Command: tt.initialCommand,
				Version: tt.version,
			}

			event := &RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventStopped,
				Version: tt.version,
			}

			got := task.ApplyRunnerEvent(event)
			if got != tt.wantUpdated {
				t.Errorf("ApplyRunnerEvent() = %v, want %v", got, tt.wantUpdated)
			}

			if task.Status != tt.wantStatus {
				t.Errorf("Status = %s, want %s", task.Status, tt.wantStatus)
			}

			if task.Command != tt.wantCommand {
				t.Errorf("Command = %s, want %s", task.Command, tt.wantCommand)
			}
		})
	}
}

func TestTask_ApplyRunnerEvent_Failed(t *testing.T) {
	tests := []struct {
		name           string
		initialStatus  TaskStatus
		initialCommand TaskCommand
		version        int64
		wantUpdated    bool
		wantStatus     TaskStatus
		wantCommand    TaskCommand
	}{
		{
			name:           "pending",
			initialStatus:  TaskStatusPending,
			initialCommand: TaskCommandRestart,
			version:        1,
			wantUpdated:    true,
			wantStatus:     TaskStatusFailed,
			wantCommand:    "",
		},
		{
			name:           "restarting",
			initialStatus:  TaskStatusRestarting,
			initialCommand: TaskCommandRestart,
			version:        1,
			wantUpdated:    true,
			wantStatus:     TaskStatusFailed,
			wantCommand:    "",
		},
		{
			name:           "running",
			initialStatus:  TaskStatusRunning,
			initialCommand: "",
			version:        1,
			wantUpdated:    true,
			wantStatus:     TaskStatusFailed,
			wantCommand:    "",
		},
		{
			name:           "running with stop command",
			initialStatus:  TaskStatusRunning,
			initialCommand: TaskCommandStop,
			version:        1,
			wantUpdated:    true,
			wantStatus:     TaskStatusFailed,
			wantCommand:    "",
		},
		{
			name:           "cancelling",
			initialStatus:  TaskStatusCancelling,
			initialCommand: TaskCommandStop,
			version:        1,
			wantUpdated:    true,
			wantStatus:     TaskStatusFailed,
			wantCommand:    "",
		},
		{
			name:           "already failed",
			initialStatus:  TaskStatusFailed,
			initialCommand: "",
			version:        1,
			wantUpdated:    false,
			wantStatus:     TaskStatusFailed,
			wantCommand:    "",
		},
		{
			name:           "completed",
			initialStatus:  TaskStatusCompleted,
			initialCommand: "",
			version:        1,
			wantUpdated:    false,
			wantStatus:     TaskStatusCompleted,
			wantCommand:    "",
		},
		{
			name:           "cancelled",
			initialStatus:  TaskStatusCancelled,
			initialCommand: "",
			version:        1,
			wantUpdated:    false,
			wantStatus:     TaskStatusCancelled,
			wantCommand:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{
				ID:      1,
				Status:  tt.initialStatus,
				Command: tt.initialCommand,
				Version: tt.version,
			}

			event := &RunnerEvent{
				TaskID:  1,
				Event:   RunnerEventFailed,
				Version: tt.version,
			}

			got := task.ApplyRunnerEvent(event)
			if got != tt.wantUpdated {
				t.Errorf("ApplyRunnerEvent() = %v, want %v", got, tt.wantUpdated)
			}

			if task.Status != tt.wantStatus {
				t.Errorf("Status = %s, want %s", task.Status, tt.wantStatus)
			}

			if task.Command != tt.wantCommand {
				t.Errorf("Command = %s, want %s", task.Command, tt.wantCommand)
			}
		})
	}
}

func TestTask_ApplyRunnerEvent_UnknownEvent(t *testing.T) {
	task := &Task{
		ID:      1,
		Status:  TaskStatusRunning,
		Command: "",
		Version: 1,
	}

	event := &RunnerEvent{
		TaskID:  1,
		Event:   RunnerEventType("unknown"),
		Version: 1,
	}

	if task.ApplyRunnerEvent(event) {
		t.Error("expected ApplyRunnerEvent to return false for unknown event type")
	}

	if task.Status != TaskStatusRunning {
		t.Errorf("expected status to remain running, got %s", task.Status)
	}
}
