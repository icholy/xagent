// Package model — TaskChange.
//
// TaskChange is the structured record of a single thing that happened to a
// task. It is the single source that projects to both the persisted audit
// log row (Log) and the in-memory change notification (Notification).
//
// One TaskChange is constructed per task per atomic change at every task-
// scoped site that today calls store.CreateLog or apiserver.Server.publish.
// The value is built inside the same transaction closure that updates the
// task, written to the log inside the transaction, and projected to a
// notification after commit.
//
// Background: proposals/draft/task-change-unifying-logs-and-notifications.md.
package model

import (
	"fmt"
	"strings"
	"time"
)

// TaskChangeKind enumerates the closed set of task-scoped changes that
// produce a log row and (sometimes) a channel notification.
type TaskChangeKind int

const (
	// TaskChangeCreated is emitted by apiserver.CreateTask.
	TaskChangeCreated TaskChangeKind = iota + 1
	// TaskChangeUpdated is emitted by apiserver.UpdateTask. Multiple field
	// edits in one call collapse into a single Updated with Changed listing
	// the mutated fields and Started=true when the call queued the runner.
	TaskChangeUpdated
	// TaskChangeCancelled is emitted by apiserver.CancelTask.
	TaskChangeCancelled
	// TaskChangeRestarted is emitted by apiserver.RestartTask.
	TaskChangeRestarted
	// TaskChangeArchived is emitted by apiserver.ArchiveTask (manual).
	TaskChangeArchived
	// TaskChangeUnarchived is emitted by apiserver.UnarchiveTask.
	TaskChangeUnarchived
	// TaskChangeAutoArchived is emitted by archiver.archive (deadline tick).
	TaskChangeAutoArchived
	// TaskChangeWoken is emitted by eventrouter.attach when a webhook event
	// matched a subscribed link and started the task.
	TaskChangeWoken
	// TaskChangeContainerStarted is emitted when the runner reports a
	// started event that the task model applied.
	TaskChangeContainerStarted
	// TaskChangeContainerExited is emitted when the runner reports a
	// stopped event that the task model applied. The resulting Status
	// distinguishes re-queue (Pending) from completion (Completed) from
	// cancellation (Cancelled).
	TaskChangeContainerExited
	// TaskChangeContainerFailed is emitted when the runner reports a
	// failed event that the task model applied.
	TaskChangeContainerFailed
)

// Actor kind constants. Used as Actor.Kind in TaskChange.Actor.
const (
	ActorKindUser     = "user"
	ActorKindAPIKey   = "api_key"
	ActorKindRunner   = "runner"
	ActorKindWebhook  = "webhook"
	ActorKindArchiver = "archiver"
)

// Actor describes the cause of a TaskChange.
type Actor struct {
	// Kind is one of the ActorKind* constants.
	Kind string
	// Name is the display name rendered into log content. Empty for
	// unattended actors with no useful name.
	Name string
	// ID is the optional stable identifier (user id, runner id, key id).
	ID string
}

// TaskChange captures one structured change to a task. It carries every
// field needed to project both the audit log row and the change
// notification, including the caller/transport context (OrgID, UserID,
// ClientID, Runner) used to address the published notification.
type TaskChange struct {
	TaskID int64
	Kind   TaskChangeKind
	Actor  Actor

	// Status is the task's status AFTER the change was applied.
	Status TaskStatus

	// Runner and Workspace are populated for the Created kind so its log
	// can render "<actor> created task on <runner>/<workspace>" without
	// re-loading the task. Runner is also used by Notification.Runner —
	// it doubles as task.PendingRunner() at the call site, which gates
	// "queued" channel messages and routes the notification.
	Runner    string
	Workspace string

	// Changed is the set of fields mutated by an Updated change.
	Changed []string

	// Started is true when an Updated change applied the Start command,
	// so the projection can render "; started" in the log and gate the
	// queued channel message.
	Started bool

	// Event is the triggering webhook event for Woken.
	Event *Event

	// OrgID / UserID / ClientID are the caller/transport context the
	// projected Notification needs to address subscribers.
	OrgID    int64
	UserID   string
	ClientID string

	// Time when the change was observed; copied verbatim into the
	// projected Notification and Log.
	Time time.Time
}

// Log projects the change into the audit-log row. Every kind logs.
func (c *TaskChange) Log() Log {
	return Log{
		TaskID:    c.TaskID,
		Type:      c.logType(),
		Content:   c.logContent(),
		CreatedAt: c.Time,
	}
}

// Notification projects the change into a model.Notification suitable
// for publication.
//
// ChannelMessage is populated only for agent-actionable kinds; log-only
// kinds leave it empty, which mcpserver.ForwardNotification gates on.
// This generalizes the PR #725 "empty ChannelMessage means silent" rule
// into a per-kind structural property.
func (c *TaskChange) Notification() Notification {
	return Notification{
		Type:           "change",
		Resources:      c.resources(),
		Time:           c.Time,
		OrgID:          c.OrgID,
		UserID:         c.UserID,
		ClientID:       c.ClientID,
		Runner:         c.Runner,
		ChannelMessage: c.channelMessage(),
	}
}

func (c *TaskChange) actorName() string {
	if c.Actor.Name != "" {
		return c.Actor.Name
	}
	return c.Actor.Kind
}

func (c *TaskChange) logType() string {
	switch c.Kind {
	case TaskChangeContainerStarted, TaskChangeContainerExited:
		return "info"
	case TaskChangeContainerFailed:
		return "error"
	default:
		return "audit"
	}
}

func (c *TaskChange) logContent() string {
	switch c.Kind {
	case TaskChangeCreated:
		return fmt.Sprintf("%s created task on %s/%s", c.actorName(), c.Runner, c.Workspace)
	case TaskChangeUpdated:
		return c.updatedLogContent()
	case TaskChangeCancelled:
		s := c.actorName() + " cancelled task"
		if c.Status == TaskStatusCancelling {
			s += "; cancellation requested"
		}
		return s
	case TaskChangeRestarted:
		return c.actorName() + " restarted task"
	case TaskChangeArchived:
		return c.actorName() + " archived task"
	case TaskChangeUnarchived:
		return c.actorName() + " unarchived task"
	case TaskChangeAutoArchived:
		return "auto-archived: archive_after deadline reached"
	case TaskChangeWoken:
		return c.wokenLogContent()
	case TaskChangeContainerStarted:
		return fmt.Sprintf("container started (status: %s)", strings.ToLower(c.Status.String()))
	case TaskChangeContainerExited:
		return fmt.Sprintf("container exited; task %s", strings.ToLower(c.Status.String()))
	case TaskChangeContainerFailed:
		return fmt.Sprintf("container failed; task %s", strings.ToLower(c.Status.String()))
	}
	return ""
}

func (c *TaskChange) updatedLogContent() string {
	prefix := c.actorName() + " updated task"
	switch {
	case len(c.Changed) > 0 && c.Started:
		return prefix + ": " + strings.Join(c.Changed, ", ") + "; started"
	case len(c.Changed) > 0:
		return prefix + ": " + strings.Join(c.Changed, ", ")
	case c.Started:
		return prefix + ": started"
	default:
		return prefix
	}
}

func (c *TaskChange) wokenLogContent() string {
	var eventID int64
	var desc, url string
	if c.Event != nil {
		eventID = c.Event.ID
		desc = c.Event.Description
		url = c.Event.URL
	}
	s := fmt.Sprintf("woken by event %d", eventID)
	if desc != "" {
		s += ": " + desc
	}
	if url != "" {
		s += " (" + url + ")"
	}
	return s
}

func (c *TaskChange) channelMessage() string {
	switch c.Kind {
	case TaskChangeCreated:
		return fmt.Sprintf("Task %d created on %s/%s.", c.TaskID, c.Runner, c.Workspace)
	case TaskChangeUpdated:
		// Only queued updates (those that handed runner work to the
		// runner) speak to the agent — matches PR #725's req.Start gate
		// via the structurally equivalent Runner check.
		if c.Runner == "" {
			return ""
		}
		if len(c.Changed) == 0 {
			return fmt.Sprintf("Task %d queued.", c.TaskID)
		}
		return fmt.Sprintf("Task %d queued: %s.", c.TaskID, strings.Join(c.Changed, ", "))
	case TaskChangeCancelled:
		// Preserve PR #725: only the Pending→Cancelled terminal branch
		// speaks; the Running→Cancelling branch stays silent and lets the
		// subsequent ContainerExited terminal event announce.
		if c.Status == TaskStatusCancelled {
			return fmt.Sprintf("Task %d cancelled.", c.TaskID)
		}
		return ""
	case TaskChangeRestarted:
		return fmt.Sprintf("Task %d restart requested.", c.TaskID)
	case TaskChangeWoken:
		return c.wokenChannelMessage()
	case TaskChangeContainerExited:
		switch c.Status {
		case TaskStatusCompleted:
			return fmt.Sprintf("Task %d completed.", c.TaskID)
		case TaskStatusCancelled:
			return fmt.Sprintf("Task %d cancelled.", c.TaskID)
		}
		return ""
	case TaskChangeContainerFailed:
		if c.Status == TaskStatusFailed {
			return fmt.Sprintf("Task %d failed.", c.TaskID)
		}
		return ""
	}
	// TaskChangeArchived, TaskChangeUnarchived, TaskChangeAutoArchived,
	// TaskChangeContainerStarted: log-only, never speak to the channel.
	return ""
}

func (c *TaskChange) wokenChannelMessage() string {
	var eventID int64
	var desc, url string
	if c.Event != nil {
		eventID = c.Event.ID
		desc = c.Event.Description
		url = c.Event.URL
	}
	s := fmt.Sprintf("Task %d woken by event %d", c.TaskID, eventID)
	if desc != "" {
		s += ": " + desc
	}
	if url != "" {
		s += " (" + url + ")"
	}
	return s
}

func (c *TaskChange) resources() []NotificationResource {
	logs := NotificationResource{Action: "appended", Type: "task_logs", ID: c.TaskID}
	switch c.Kind {
	case TaskChangeCreated:
		return []NotificationResource{
			{Action: "created", Type: "task", ID: c.TaskID},
			logs,
		}
	case TaskChangeArchived, TaskChangeAutoArchived:
		return []NotificationResource{
			{Action: "archived", Type: "task", ID: c.TaskID},
			logs,
		}
	case TaskChangeUnarchived:
		return []NotificationResource{
			{Action: "unarchived", Type: "task", ID: c.TaskID},
			logs,
		}
	case TaskChangeCancelled:
		return []NotificationResource{
			{Action: "cancelled", Type: "task", ID: c.TaskID},
			logs,
		}
	case TaskChangeRestarted:
		return []NotificationResource{
			{Action: "restarted", Type: "task", ID: c.TaskID},
			logs,
		}
	case TaskChangeWoken:
		var eventID int64
		if c.Event != nil {
			eventID = c.Event.ID
		}
		return []NotificationResource{
			{Action: "updated", Type: "task", ID: c.TaskID},
			{Action: "updated", Type: "event", ID: eventID},
			logs,
		}
	default:
		// TaskChangeUpdated and Container* kinds emit an "updated task" row.
		return []NotificationResource{
			{Action: "updated", Type: "task", ID: c.TaskID},
			logs,
		}
	}
}
