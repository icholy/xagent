package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TaskStatus represents the current state of a task.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusRunning    TaskStatus = "running"
	TaskStatusRestarting TaskStatus = "restarting"
	TaskStatusCancelling TaskStatus = "cancelling"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusCancelled  TaskStatus = "cancelled"
	TaskStatusArchived   TaskStatus = "archived"
)

// TaskCommand represents a command to be executed by the runner.
type TaskCommand string

const (
	TaskCommandRestart TaskCommand = "restart"
	TaskCommandStop    TaskCommand = "stop"
)

// Instruction represents a task instruction with text and optional source URL.
type Instruction struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

// Proto converts an Instruction to its protobuf representation.
func (i *Instruction) Proto() *xagentv1.Instruction {
	return &xagentv1.Instruction{
		Text: i.Text,
		Url:  i.URL,
	}
}

// InstructionFromProto converts a protobuf Instruction to a model Instruction.
func InstructionFromProto(pb *xagentv1.Instruction) Instruction {
	return Instruction{
		Text: pb.Text,
		URL:  pb.Url,
	}
}

// Task represents a task in the system.
type Task struct {
	ID           int64         `json:"id"`
	Name         string        `json:"name"`
	Parent       int64         `json:"parent"`
	Workspace    string        `json:"workspace"`
	Instructions []Instruction `json:"instructions"`
	Status       TaskStatus    `json:"status"`
	Command      TaskCommand   `json:"command"`
	Version      int64         `json:"version"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// Proto converts a Task to its protobuf representation.
func (t *Task) Proto() *xagentv1.Task {
	instructions := make([]*xagentv1.Instruction, len(t.Instructions))
	for i, inst := range t.Instructions {
		instructions[i] = inst.Proto()
	}
	return &xagentv1.Task{
		Id:           t.ID,
		Name:         t.Name,
		Parent:       t.Parent,
		Workspace:    t.Workspace,
		Instructions: instructions,
		Status:       string(t.Status),
		Command:      string(t.Command),
		Version:      t.Version,
		CreatedAt:    timestamppb.New(t.CreatedAt),
		UpdatedAt:    timestamppb.New(t.UpdatedAt),
	}
}

// TaskFromProto converts a protobuf Task to a model Task.
func TaskFromProto(pb *xagentv1.Task) *Task {
	instructions := make([]Instruction, len(pb.Instructions))
	for i, inst := range pb.Instructions {
		instructions[i] = InstructionFromProto(inst)
	}
	var createdAt, updatedAt time.Time
	if pb.CreatedAt != nil {
		createdAt = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		updatedAt = pb.UpdatedAt.AsTime()
	}
	return &Task{
		ID:           pb.Id,
		Name:         pb.Name,
		Parent:       pb.Parent,
		Workspace:    pb.Workspace,
		Instructions: instructions,
		Status:       TaskStatus(pb.Status),
		Command:      TaskCommand(pb.Command),
		Version:      pb.Version,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
}

// RunnerEventType represents the type of event reported by the runner.
type RunnerEventType string

const (
	RunnerEventStarted RunnerEventType = "started"
	RunnerEventStopped RunnerEventType = "stopped"
	RunnerEventFailed  RunnerEventType = "failed"
)

// RunnerEvent represents an event from the runner about a task's container.
type RunnerEvent struct {
	TaskID    int64
	Event     RunnerEventType
	Version   int64
	Reconcile bool
}

// Proto converts a RunnerEvent to its protobuf representation.
func (r *RunnerEvent) Proto() *xagentv1.RunnerEvent {
	return &xagentv1.RunnerEvent{
		TaskId:    r.TaskID,
		Event:     string(r.Event),
		Version:   r.Version,
		Reconcile: r.Reconcile,
	}
}

// RunnerEventFromProto converts a protobuf RunnerEvent to a model RunnerEvent.
func RunnerEventFromProto(pb *xagentv1.RunnerEvent) RunnerEvent {
	return RunnerEvent{
		TaskID:    pb.TaskId,
		Event:     RunnerEventType(pb.Event),
		Version:   pb.Version,
		Reconcile: pb.Reconcile,
	}
}

// ApplyRunnerEvent applies a runner event to the task, updating its status
// and command fields according to the state machine rules defined in RFC #149.
// Returns true if the task was updated, false otherwise.
func (t *Task) ApplyRunnerEvent(e *RunnerEvent) bool {
	// TODO: Reconciliation events require special handling
	if e.Reconcile {
		return false
	}

	// Check version match (version 0 bypasses check for spontaneous failures)
	if e.Version != 0 && e.Version != t.Version {
		return false
	}

	var newStatus TaskStatus
	var clearCommand bool

	switch e.Event {
	case RunnerEventStarted:
		switch t.Status {
		case TaskStatusPending, TaskStatusRestarting:
			if t.Command == TaskCommandRestart {
				newStatus = TaskStatusRunning
				clearCommand = true
			}
		case TaskStatusRunning:
			if t.Command == TaskCommandRestart {
				newStatus = TaskStatusRunning
				clearCommand = true
			}
		default:
			return false
		}

	case RunnerEventStopped:
		switch t.Status {
		case TaskStatusRunning:
			if t.Command == TaskCommandStop {
				newStatus = TaskStatusCancelled
				clearCommand = true
			} else if t.Command == "" {
				newStatus = TaskStatusCompleted
			} else {
				return false
			}
		case TaskStatusCancelling:
			if t.Command == TaskCommandStop {
				newStatus = TaskStatusCancelled
				clearCommand = true
			} else {
				return false
			}
		default:
			return false
		}

	case RunnerEventFailed:
		// Failed events always result in failed status
		switch t.Status {
		case TaskStatusPending, TaskStatusRestarting, TaskStatusRunning, TaskStatusCancelling:
			newStatus = TaskStatusFailed
			clearCommand = true
		default:
			return false
		}

	default:
		return false
	}

	// Apply the updates
	if newStatus == "" {
		return false
	}

	t.Status = newStatus
	if clearCommand {
		t.Command = ""
	}
	return true
}
