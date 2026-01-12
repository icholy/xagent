// Package statemachine implements task state transitions based on runner events.
package statemachine

// Task status constants
const (
	StatusPending     = "pending"
	StatusRestarting  = "restarting"
	StatusRunning     = "running"
	StatusCancelling  = "cancelling"
	StatusCancelled   = "cancelled"
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
)

// Command constants
const (
	CommandRestart = "restart"
	CommandStop    = "stop"
)

// Event constants
const (
	EventStarted = "started"
	EventStopped = "stopped"
	EventFailed  = "failed"
)

// Task represents the state of a task for state machine transitions.
type Task struct {
	Status  string
	Command string
	Version int64
}

// RunnerEvent represents an event from the runner.
type RunnerEvent struct {
	Event     string
	Version   int64
	Reconcile bool
}

// Update applies a runner event to a task and returns true if the task was modified.
// The Task struct is modified in place with the new status and command.
//
// State transitions follow these rules:
// - A failed event always results in failed status, regardless of pending command
// - Version 0 in the event is a bypass (for spontaneous failures)
// - Events with mismatched versions (non-zero, non-matching) are ignored
// - Reconcile events only update if the task isn't already in the expected state
func Update(task *Task, event RunnerEvent) bool {
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
func applyEvent(task *Task, event RunnerEvent) bool {
	oldStatus := task.Status
	oldCommand := task.Command

	switch event.Event {
	case EventFailed:
		// Failed always results in failed status, clears command if version matches
		task.Status = StatusFailed
		if event.Version == task.Version || event.Version == 0 {
			task.Command = ""
		}

	case EventStarted:
		switch task.Status {
		case StatusPending:
			if task.Command == CommandRestart {
				task.Status = StatusRunning
				task.Command = ""
			}
		case StatusRestarting:
			if task.Command == CommandRestart {
				task.Status = StatusRunning
				task.Command = ""
			}
		case StatusRunning:
			if task.Command == CommandRestart {
				task.Status = StatusRunning
				task.Command = ""
			}
		}

	case EventStopped:
		switch task.Status {
		case StatusRunning:
			if task.Command == CommandStop {
				task.Status = StatusCancelled
				task.Command = ""
			} else if task.Command == "" {
				task.Status = StatusCompleted
			}
		case StatusCancelling:
			if task.Command == CommandStop {
				task.Status = StatusCancelled
				task.Command = ""
			}
		case StatusPending:
			if task.Command == CommandRestart {
				task.Status = StatusFailed
				task.Command = ""
			}
		case StatusRestarting:
			if task.Command == CommandRestart {
				task.Status = StatusFailed
				task.Command = ""
			}
		}
	}

	return task.Status != oldStatus || task.Command != oldCommand
}

// applyReconcileEvent applies a reconciliation event to the task.
// Reconcile events only update if the task doesn't already reflect the state.
func applyReconcileEvent(task *Task, event RunnerEvent) bool {
	oldStatus := task.Status
	oldCommand := task.Command

	switch event.Event {
	case EventFailed:
		// Reconcile failed only updates running tasks
		if task.Status == StatusRunning {
			task.Status = StatusFailed
			task.Command = ""
		}

	case EventStarted:
		// Reconcile started only updates pending/restarting tasks
		switch task.Status {
		case StatusPending, StatusRestarting:
			if task.Command == CommandRestart {
				task.Status = StatusRunning
				task.Command = ""
			}
		}

	case EventStopped:
		// Reconcile stopped only updates running tasks
		if task.Status == StatusRunning {
			if task.Command == CommandStop {
				task.Status = StatusCancelled
				task.Command = ""
			} else if task.Command == "" {
				task.Status = StatusCompleted
			}
		}
	}

	return task.Status != oldStatus || task.Command != oldCommand
}
