package model

import (
	"fmt"
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

//go:generate stringer -type=TaskStatus -trimprefix=TaskStatus

// TaskStatus represents the current state of a task.
type TaskStatus int32

const (
	TaskStatusPending    TaskStatus = TaskStatus(xagentv1.TaskStatus_PENDING)
	TaskStatusRunning    TaskStatus = TaskStatus(xagentv1.TaskStatus_RUNNING)
	TaskStatusRestarting TaskStatus = TaskStatus(xagentv1.TaskStatus_RESTARTING)
	TaskStatusCancelling TaskStatus = TaskStatus(xagentv1.TaskStatus_CANCELLING)
	TaskStatusCompleted  TaskStatus = TaskStatus(xagentv1.TaskStatus_COMPLETED)
	TaskStatusFailed     TaskStatus = TaskStatus(xagentv1.TaskStatus_FAILED)
	TaskStatusCancelled  TaskStatus = TaskStatus(xagentv1.TaskStatus_CANCELLED)
)

//go:generate stringer -type=TaskCommand -trimprefix=TaskCommand

// TaskCommand represents a command to be executed by the runner.
type TaskCommand int32

const (
	TaskCommandNone    TaskCommand = TaskCommand(xagentv1.TaskCommand_NONE)
	TaskCommandRestart TaskCommand = TaskCommand(xagentv1.TaskCommand_RESTART)
	TaskCommandStop    TaskCommand = TaskCommand(xagentv1.TaskCommand_STOP)
	TaskCommandStart   TaskCommand = TaskCommand(xagentv1.TaskCommand_START)
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
	Runner       string        `json:"runner"`
	Workspace    string        `json:"workspace"`
	Instructions []Instruction `json:"instructions"`
	Status       TaskStatus    `json:"status"`
	Command      TaskCommand   `json:"command"`
	Version      int64         `json:"version"`
	OrgID        int64         `json:"org_id"`
	Archived     bool          `json:"archived"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// Proto converts a Task to its protobuf representation.
func (t *Task) Proto(baseURL string) *xagentv1.Task {
	instructions := make([]*xagentv1.Instruction, len(t.Instructions))
	for i, inst := range t.Instructions {
		instructions[i] = inst.Proto()
	}
	return &xagentv1.Task{
		Id:           t.ID,
		Name:         t.Name,
		Parent:       t.Parent,
		Runner:       t.Runner,
		Workspace:    t.Workspace,
		Instructions: instructions,
		Status:       xagentv1.TaskStatus(t.Status),
		Command:      xagentv1.TaskCommand(t.Command),
		Version:      t.Version,
		Archived:     t.Archived,
		Url:          fmt.Sprintf("%s/tasks/%d", baseURL, t.ID),
		CreatedAt:    timestamppb.New(t.CreatedAt),
		UpdatedAt:    timestamppb.New(t.UpdatedAt),
		Actions: &xagentv1.TaskActions{
			Archive:   t.CanArchive(),
			Unarchive: t.CanUnarchive(),
			Cancel:    t.CanCancel(),
			Restart:   t.CanRestart(),
			Start:     t.CanStart(),
		},
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
		Runner:       pb.Runner,
		Workspace:    pb.Workspace,
		Instructions: instructions,
		Status:       TaskStatus(pb.Status),
		Command:      TaskCommand(pb.Command),
		Version:      pb.Version,
		Archived:     pb.Archived,
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

	switch e.Event {
	case RunnerEventStarted:
		return t.applyRunnerEventStarted()
	case RunnerEventStopped:
		return t.applyRunnerEventStopped()
	case RunnerEventFailed:
		return t.applyRunnerEventFailed()
	default:
		return false
	}
}

func (t *Task) applyRunnerEventStarted() bool {
	// If an archived task's container starts, cancel it
	if t.Archived {
		t.Status = TaskStatusCancelling
		t.Command = TaskCommandStop
		t.Version++
		return true
	}
	switch t.Status {
	case TaskStatusPending, TaskStatusRestarting, TaskStatusRunning:
		if t.Command == TaskCommandRestart || t.Command == TaskCommandStart {
			t.Status = TaskStatusRunning
			t.Command = TaskCommandNone
			return true
		}
		return false
	case TaskStatusCancelled:
		t.Status = TaskStatusCancelling
		t.Command = TaskCommandStop
		t.Version++
		return true
	default:
		return false
	}
}

func (t *Task) applyRunnerEventStopped() bool {
	switch t.Status {
	case TaskStatusRunning:
		if t.Command == TaskCommandStop {
			t.Status = TaskStatusCancelled
			t.Command = TaskCommandNone
			return true
		}
		if t.Command == TaskCommandStart {
			// Container finished, but start command pending
			// Go back to pending so runner picks it up and starts a new container
			t.Status = TaskStatusPending
			// Keep command as "start" so runner will start it
			return true
		}
		if t.Command == TaskCommandNone {
			t.Status = TaskStatusCompleted
			return true
		}
		return false
	case TaskStatusCancelling:
		if t.Command == TaskCommandStop {
			t.Status = TaskStatusCancelled
			t.Command = TaskCommandNone
			return true
		}
		return false
	default:
		return false
	}
}

func (t *Task) applyRunnerEventFailed() bool {
	switch t.Status {
	case TaskStatusPending, TaskStatusRestarting, TaskStatusRunning, TaskStatusCancelling:
		t.Status = TaskStatusFailed
		t.Command = TaskCommandNone
		return true
	default:
		return false
	}
}

// CanArchive returns true if the task can be archived.
func (t *Task) CanArchive() bool {
	if t.Archived || t.Command != TaskCommandNone {
		return false
	}
	switch t.Status {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

// Archive marks the task as archived.
// Returns true if the transition is valid and was applied.
// Only valid from completed, failed, or cancelled status.
func (t *Task) Archive() bool {
	if !t.CanArchive() {
		return false
	}
	t.Archived = true
	return true
}

// CanUnarchive returns true if the task can be unarchived.
func (t *Task) CanUnarchive() bool {
	return t.Archived
}

// Unarchive marks the task as no longer archived.
// Returns true if the task was archived and is now unarchived.
func (t *Task) Unarchive() bool {
	if !t.CanUnarchive() {
		return false
	}
	t.Archived = false
	return true
}

// CanCancel returns true if the task can be cancelled.
func (t *Task) CanCancel() bool {
	if t.Archived {
		return false
	}
	switch t.Status {
	case TaskStatusRunning, TaskStatusRestarting, TaskStatusPending:
		return true
	default:
		return false
	}
}

// Cancel transitions the task to cancelling/cancelled status and sets the stop command.
// Returns true if the transition is valid and was applied.
// For running or restarting tasks: sets status to cancelling, command to stop, increments version.
// For pending tasks: sets status to cancelled directly (no runner action needed).
func (t *Task) Cancel() bool {
	if !t.CanCancel() {
		return false
	}
	switch t.Status {
	case TaskStatusRunning, TaskStatusRestarting:
		t.Status = TaskStatusCancelling
		t.Command = TaskCommandStop
		t.Version++
	case TaskStatusPending:
		t.Status = TaskStatusCancelled
		t.Command = TaskCommandNone
	}
	return true
}

// CanRestart returns true if the task can be restarted.
func (t *Task) CanRestart() bool {
	if t.Archived {
		return false
	}
	switch t.Status {
	case TaskStatusRunning, TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

// Restart transitions the task to pending/restarting status and sets the restart command.
// Returns true if the transition is valid and was applied.
// For running tasks: sets status to restarting, command to restart, increments version.
// For completed, failed, or cancelled tasks: sets status to pending, command to restart, increments version.
func (t *Task) Restart() bool {
	if !t.CanRestart() {
		return false
	}
	switch t.Status {
	case TaskStatusRunning:
		t.Status = TaskStatusRestarting
	default:
		t.Status = TaskStatusPending
	}
	t.Command = TaskCommandRestart
	t.Version++
	return true
}

// CanStart returns true if the task can be started.
func (t *Task) CanStart() bool {
	if t.Archived {
		return false
	}
	switch t.Status {
	case TaskStatusRunning, TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

// Start sets the start command without interrupting a running task.
// Returns true if the transition is valid and was applied.
// For running tasks: sets command to start (container continues, will restart after exit).
// For completed, failed, or cancelled tasks: sets status to pending, command to start, increments version.
func (t *Task) Start() bool {
	if !t.CanStart() {
		return false
	}
	if t.Status != TaskStatusRunning {
		t.Status = TaskStatusPending
	}
	t.Command = TaskCommandStart
	t.Version++
	return true
}
