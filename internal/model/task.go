package model

import (
	"time"

	"github.com/icholy/xagent/internal/auth/authscope"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

//go:generate stringer -type=TaskStatus -trimprefix=TaskStatus

// TaskStatus represents the current state of a task.
type TaskStatus int32

const (
	TaskStatusUnspecified TaskStatus = TaskStatus(xagentv1.TaskStatus_UNSPECIFIED)
	TaskStatusPending     TaskStatus = TaskStatus(xagentv1.TaskStatus_PENDING)
	TaskStatusRunning     TaskStatus = TaskStatus(xagentv1.TaskStatus_RUNNING)
	TaskStatusRestarting  TaskStatus = TaskStatus(xagentv1.TaskStatus_RESTARTING)
	TaskStatusCancelling  TaskStatus = TaskStatus(xagentv1.TaskStatus_CANCELLING)
	TaskStatusCompleted   TaskStatus = TaskStatus(xagentv1.TaskStatus_COMPLETED)
	TaskStatusFailed      TaskStatus = TaskStatus(xagentv1.TaskStatus_FAILED)
	TaskStatusCancelled   TaskStatus = TaskStatus(xagentv1.TaskStatus_CANCELLED)
)

// Label renders a TaskStatus for a lifecycle payload, mapping the zero
// (unspecified) status to the empty string — e.g. a freshly created task has no
// prior status.
func (s TaskStatus) Label() string {
	if s == TaskStatusUnspecified {
		return ""
	}
	return s.String()
}

//go:generate stringer -type=TaskCommand -trimprefix=TaskCommand

// TaskCommand represents a command to be executed by the runner.
type TaskCommand int32

const (
	TaskCommandNone    TaskCommand = TaskCommand(xagentv1.TaskCommand_NONE)
	TaskCommandRestart TaskCommand = TaskCommand(xagentv1.TaskCommand_RESTART)
	TaskCommandStop    TaskCommand = TaskCommand(xagentv1.TaskCommand_STOP)
	TaskCommandStart   TaskCommand = TaskCommand(xagentv1.TaskCommand_START)
)

// Task represents a task in the system. Instructions are no longer a Task field —
// they are instruction events in the task's stream (see InstructionPayload).
type Task struct {
	ID        int64       `json:"id"`
	Name      string      `json:"name"`
	Runner    string      `json:"runner"`
	Workspace string      `json:"workspace"`
	Status    TaskStatus  `json:"status"`
	Command   TaskCommand `json:"command"`
	Version   int64       `json:"version"`
	OrgID     int64       `json:"org_id"`
	Archived  bool        `json:"archived"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	// AutoArchive controls auto-archive after the task reaches a terminal
	// status. 0 = never (default); <0 = archive immediately; >0 = delay.
	AutoArchive time.Duration `json:"auto_archive,omitempty"`
}

// ScopeAttr returns the authscope attributes describing this task, for the
// post-load Allow check in the apiserver task handlers. Centralizing the set
// here keeps a handler from forgetting one (especially task.archived): own-task
// matching rides on task.id and archive-based revocation on task.archived. See
// proposals/implemented/eliminate-runner-socket-proxy.md §5.
func (t *Task) ScopeAttr() []authscope.Attr {
	return []authscope.Attr{
		authscope.WithTaskID(t.ID),
		authscope.WithTaskArchived(t.Archived),
	}
}

// Proto converts a Task to its protobuf representation.
func (t *Task) Proto(baseURL string) *xagentv1.Task {
	return &xagentv1.Task{
		Id:          t.ID,
		Name:        t.Name,
		Runner:      t.Runner,
		Workspace:   t.Workspace,
		Status:      xagentv1.TaskStatus(t.Status),
		Command:     xagentv1.TaskCommand(t.Command),
		Version:     t.Version,
		Archived:    t.Archived,
		Url:         TaskURL(baseURL, t.ID, t.OrgID),
		CreatedAt:   timestamppb.New(t.CreatedAt),
		UpdatedAt:   timestamppb.New(t.UpdatedAt),
		AutoArchive: durationpb.New(t.AutoArchive),
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
	var createdAt, updatedAt time.Time
	if pb.CreatedAt != nil {
		createdAt = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		updatedAt = pb.UpdatedAt.AsTime()
	}
	return &Task{
		ID:          pb.Id,
		Name:        pb.Name,
		Runner:      pb.Runner,
		Workspace:   pb.Workspace,
		Status:      TaskStatus(pb.Status),
		Command:     TaskCommand(pb.Command),
		Version:     pb.Version,
		Archived:    pb.Archived,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		AutoArchive: pb.AutoArchive.AsDuration(),
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
// Returns true if the task was updated, false otherwise. It is a thin wrapper
// over ApplyRunnerEventLifecycle, which routes the transition through the
// ApplyLifecycleEvent fold.
func (t *Task) ApplyRunnerEvent(e *RunnerEvent) bool {
	_, ok := t.ApplyRunnerEventLifecycle(e)
	return ok
}

// ApplyRunnerEventLifecycle is the runner-event adapter over ApplyLifecycleEvent.
// It enforces the reconcile/version guard, maps the runner event to its matching
// SANDBOX_* lifecycle kind, then delegates the transition to the fold. It returns
// the constructed lifecycle payload (for the event the caller appends beside the
// updated row) and whether the transition applied.
func (t *Task) ApplyRunnerEventLifecycle(e *RunnerEvent) (*LifecyclePayload, bool) {
	// TODO: Reconciliation events require special handling
	if e.Reconcile {
		return nil, false
	}
	// Check version match (version 0 bypasses check for spontaneous failures)
	if e.Version != 0 && e.Version != t.Version {
		return nil, false
	}
	kind, message, ok := e.Event.lifecycleKind()
	if !ok {
		return nil, false
	}
	return t.ApplyLifecycleEvent(kind, RunnerActor, message)
}

// lifecycleKind maps a runner event type to its sandbox lifecycle kind and the
// message that rides on the resulting event. Returns false for event types with
// no lifecycle home.
func (e RunnerEventType) lifecycleKind() (LifecycleKind, string, bool) {
	switch e {
	case RunnerEventStarted:
		return LifecycleKindSandboxStarted, "", true
	case RunnerEventStopped:
		return LifecycleKindSandboxExited, "", true
	case RunnerEventFailed:
		// The container failure detail rides in the SANDBOX_FAILED message field
		// (the old `error` log content).
		return LifecycleKindSandboxFailed, "container failed", true
	default:
		return LifecycleKindUnspecified, "", false
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

// ApplyLifecycleEvent is the single place that applies a lifecycle kind's full
// effect to the task's materialized state — status, archived, command, and
// version — enforcing the same guards the per-mutation methods (Cancel, Start,
// Archive, …) do. It returns the lifecycle payload describing the transition
// (from/to taken before/after the change) and whether the effect applied; when a
// guard rejects the transition the task is left untouched and (nil, false) is
// returned.
//
// Routing every transition through this fold keeps the materialized status from
// drifting from the lifecycle event appended beside it: the event and the
// projection come from one call (#952). Callers persist the task and append
// eventFromLifecycle(task, payload) in the same transaction.
func (t *Task) ApplyLifecycleEvent(kind LifecycleKind, actor Actor, message string) (*LifecyclePayload, bool) {
	from := t.Status
	var ok bool
	switch kind {
	case LifecycleKindCreated:
		// A freshly created task already carries its initial status; CREATED
		// records where it landed with no prior status (from stays unspecified).
		from = TaskStatusUnspecified
		ok = true
	case LifecycleKindUpdated:
		// A rename / metadata update is not a status transition (from == to).
		ok = true
	case LifecycleKindCancelled:
		ok = t.Cancel()
	case LifecycleKindRestarted:
		ok = t.Start()
	case LifecycleKindArchived, LifecycleKindAutoArchived:
		ok = t.Archive()
	case LifecycleKindUnarchived:
		ok = t.Unarchive()
	case LifecycleKindSandboxStarted:
		ok = t.applyRunnerEventStarted()
	case LifecycleKindSandboxExited:
		ok = t.applyRunnerEventStopped()
	case LifecycleKindSandboxFailed:
		ok = t.applyRunnerEventFailed()
	default:
		return nil, false
	}
	if !ok {
		return nil, false
	}
	return &LifecyclePayload{
		Kind:       kind,
		Actor:      actor,
		FromStatus: from.Label(),
		ToStatus:   t.Status.Label(),
		Message:    message,
	}, true
}

// IsDone reports whether the task has finished its run: completed, failed,
// or cancelled. A done task can still be restarted via Start/Restart, so
// this isn't an absorbing state — just a snapshot that the current run
// reached an end.
func (t *Task) IsDone() bool {
	return t.Status == TaskStatusCompleted ||
		t.Status == TaskStatusFailed ||
		t.Status == TaskStatusCancelled
}

// CanArchive returns true if the task can be archived.
func (t *Task) CanArchive() bool {
	if t.Archived || t.Command != TaskCommandNone {
		return false
	}
	return t.IsDone()
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

// Unarchive marks the task as no longer archived. Also clears any
// auto_archive timeout (sets it to 0 = never) so the archiver worker doesn't
// immediately re-archive the task on its next tick.
// Returns true if the task was archived and is now unarchived.
func (t *Task) Unarchive() bool {
	if !t.CanUnarchive() {
		return false
	}
	t.Archived = false
	t.AutoArchive = 0
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

// PendingRunner returns the runner that has pending work for this task, or ""
// if no runner action is needed. A runner has work when the task has a command
// and is not archived — the same condition as ListTasksForRunner.
func (t *Task) PendingRunner() string {
	if t.Command == TaskCommandNone || t.Archived {
		return ""
	}
	return t.Runner
}
