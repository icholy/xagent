// Package statemachine implements task state transitions based on runner events.
package statemachine

import "github.com/icholy/xagent/internal/model"

// Update applies a runner event to a task and returns true if the task was modified.
// The Task struct is modified in place with the new status and command.
//
// State transitions follow these rules:
// - A failed event always results in failed status, regardless of pending command
// - Version 0 in the event is a bypass (for spontaneous failures)
// - Events with mismatched versions (non-zero, non-matching) are ignored
// - Reconcile events only update if the task isn't already in the expected state
func Update(task *model.Task, event *model.RunnerEvent) bool {
	// Version check: if event version is non-zero, it must match task version
	// Version 0 is a bypass (for spontaneous failures)
	if event.Version != 0 && event.Version != task.Version {
		return false
	}

	// For reconcile events, check if task already reflects the expected state
	if event.Reconcile {
		return applyReconcileEvent(task, event)
	}

	return applyEvent(task, event)
}

// applyEvent applies a real-time event to the task.
func applyEvent(task *model.Task, event *model.RunnerEvent) bool {
	oldStatus := task.Status
	oldCommand := task.Command

	switch event.Event {
	case model.RunnerEventFailed:
		// Failed always results in failed status, clears command if version matches
		task.Status = model.TaskStatusFailed
		if event.Version == task.Version || event.Version == 0 {
			task.Command = ""
		}

	case model.RunnerEventStarted:
		switch task.Status {
		case model.TaskStatusPending:
			if task.Command == model.TaskCommandRestart {
				task.Status = model.TaskStatusRunning
				task.Command = ""
			}
		case model.TaskStatusRestarting:
			if task.Command == model.TaskCommandRestart {
				task.Status = model.TaskStatusRunning
				task.Command = ""
			}
		case model.TaskStatusRunning:
			if task.Command == model.TaskCommandRestart {
				task.Status = model.TaskStatusRunning
				task.Command = ""
			}
		}

	case model.RunnerEventStopped:
		switch task.Status {
		case model.TaskStatusRunning:
			if task.Command == model.TaskCommandStop {
				task.Status = model.TaskStatusCancelled
				task.Command = ""
			} else if task.Command == "" {
				task.Status = model.TaskStatusCompleted
			}
		case model.TaskStatusCancelling:
			if task.Command == model.TaskCommandStop {
				task.Status = model.TaskStatusCancelled
				task.Command = ""
			}
		case model.TaskStatusPending:
			if task.Command == model.TaskCommandRestart {
				task.Status = model.TaskStatusFailed
				task.Command = ""
			}
		case model.TaskStatusRestarting:
			if task.Command == model.TaskCommandRestart {
				task.Status = model.TaskStatusFailed
				task.Command = ""
			}
		}
	}

	return task.Status != oldStatus || task.Command != oldCommand
}

// applyReconcileEvent applies a reconciliation event to the task.
// Reconcile events only update if the task doesn't already reflect the state.
func applyReconcileEvent(task *model.Task, event *model.RunnerEvent) bool {
	oldStatus := task.Status
	oldCommand := task.Command

	switch event.Event {
	case model.RunnerEventFailed:
		// Reconcile failed only updates running tasks
		if task.Status == model.TaskStatusRunning {
			task.Status = model.TaskStatusFailed
			task.Command = ""
		}

	case model.RunnerEventStarted:
		// Reconcile started only updates pending/restarting tasks
		switch task.Status {
		case model.TaskStatusPending, model.TaskStatusRestarting:
			if task.Command == model.TaskCommandRestart {
				task.Status = model.TaskStatusRunning
				task.Command = ""
			}
		}

	case model.RunnerEventStopped:
		// Reconcile stopped only updates running tasks
		if task.Status == model.TaskStatusRunning {
			if task.Command == model.TaskCommandStop {
				task.Status = model.TaskStatusCancelled
				task.Command = ""
			} else if task.Command == "" {
				task.Status = model.TaskStatusCompleted
			}
		}
	}

	return task.Status != oldStatus || task.Command != oldCommand
}
