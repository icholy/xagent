package model

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"
	"gotest.tools/v3/assert"
)

// nonzeroTime is a fixed instant used so tests can compare Notification/Log
// projections without dealing with time.Now() drift.
var nonzeroTime = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

func TestTaskChange_Log_Projection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		change      TaskChange
		wantType    string
		wantContent string
	}{
		{
			name: "Created includes runner/workspace",
			change: TaskChange{
				TaskID:    7,
				Kind:      TaskChangeCreated,
				Actor:     Actor{Kind: ActorKindUser, Name: "alice"},
				Runner:    "r",
				Workspace: "w",
			},
			wantType:    "audit",
			wantContent: "alice created task on r/w",
		},
		{
			name: "Updated lists changed fields",
			change: TaskChange{
				TaskID:  7,
				Kind:    TaskChangeUpdated,
				Actor:   Actor{Kind: ActorKindUser, Name: "alice"},
				Changed: []string{"name", "instructions"},
			},
			wantType:    "audit",
			wantContent: "alice updated task: name, instructions",
		},
		{
			name: "Updated with Start appends started",
			change: TaskChange{
				TaskID:  7,
				Kind:    TaskChangeUpdated,
				Actor:   Actor{Kind: ActorKindUser, Name: "alice"},
				Changed: []string{"name", "instructions"},
				Started: true,
			},
			wantType:    "audit",
			wantContent: "alice updated task: name, instructions; started",
		},
		{
			name: "Updated with only Start",
			change: TaskChange{
				TaskID:  7,
				Kind:    TaskChangeUpdated,
				Actor:   Actor{Kind: ActorKindUser, Name: "alice"},
				Started: true,
			},
			wantType:    "audit",
			wantContent: "alice updated task: started",
		},
		{
			name: "Cancelled when terminal",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeCancelled,
				Actor:  Actor{Kind: ActorKindUser, Name: "alice"},
				Status: TaskStatusCancelled,
			},
			wantType:    "audit",
			wantContent: "alice cancelled task",
		},
		{
			name: "Cancelled when only cancellation requested",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeCancelled,
				Actor:  Actor{Kind: ActorKindUser, Name: "alice"},
				Status: TaskStatusCancelling,
			},
			wantType:    "audit",
			wantContent: "alice cancelled task; cancellation requested",
		},
		{
			name: "Restarted",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeRestarted,
				Actor:  Actor{Kind: ActorKindUser, Name: "alice"},
			},
			wantType:    "audit",
			wantContent: "alice restarted task",
		},
		{
			name: "Archived",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeArchived,
				Actor:  Actor{Kind: ActorKindUser, Name: "alice"},
			},
			wantType:    "audit",
			wantContent: "alice archived task",
		},
		{
			name: "Unarchived",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeUnarchived,
				Actor:  Actor{Kind: ActorKindUser, Name: "alice"},
			},
			wantType:    "audit",
			wantContent: "alice unarchived task",
		},
		{
			name: "AutoArchived",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeAutoArchived,
				Actor:  Actor{Kind: ActorKindArchiver},
			},
			wantType:    "audit",
			wantContent: "auto-archived: archive_after deadline reached",
		},
		{
			name: "Woken includes description and URL",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeWoken,
				Actor:  Actor{Kind: ActorKindWebhook},
				Event: &Event{
					ID:          42,
					Description: "PR comment from alice",
					URL:         "https://github.com/x/y/pull/1#issuecomment-1",
				},
			},
			wantType:    "audit",
			wantContent: "woken by event 42: PR comment from alice (https://github.com/x/y/pull/1#issuecomment-1)",
		},
		{
			name: "Woken with no description or URL",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeWoken,
				Actor:  Actor{Kind: ActorKindWebhook},
				Event:  &Event{ID: 42},
			},
			wantType:    "audit",
			wantContent: "woken by event 42",
		},
		{
			name: "ContainerStarted notes status",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeContainerStarted,
				Actor:  Actor{Kind: ActorKindRunner, Name: "r"},
				Status: TaskStatusRunning,
			},
			wantType:    "info",
			wantContent: "container started (status: running)",
		},
		{
			name: "ContainerExited with terminal Completed status",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeContainerExited,
				Actor:  Actor{Kind: ActorKindRunner, Name: "r"},
				Status: TaskStatusCompleted,
			},
			wantType:    "info",
			wantContent: "container exited; task completed",
		},
		{
			name: "ContainerExited with re-queue distinguishes pending",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeContainerExited,
				Actor:  Actor{Kind: ActorKindRunner, Name: "r"},
				Status: TaskStatusPending,
			},
			wantType:    "info",
			wantContent: "container exited; task pending",
		},
		{
			name: "ContainerFailed",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeContainerFailed,
				Actor:  Actor{Kind: ActorKindRunner, Name: "r"},
				Status: TaskStatusFailed,
			},
			wantType:    "error",
			wantContent: "container failed; task failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := tt.change
			c.Time = nonzeroTime
			got := c.Log()
			assert.Equal(t, got.TaskID, c.TaskID)
			assert.Equal(t, got.Type, tt.wantType)
			assert.Equal(t, got.Content, tt.wantContent)
			assert.Equal(t, got.CreatedAt, nonzeroTime)
		})
	}
}

func TestTaskChange_Notification_ChannelMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		change  TaskChange
		wantMsg string
	}{
		{
			name: "Created speaks",
			change: TaskChange{
				TaskID:    7,
				Kind:      TaskChangeCreated,
				Actor:     Actor{Kind: ActorKindUser, Name: "alice"},
				Runner:    "r",
				Workspace: "w",
			},
			wantMsg: "Task 7 created on r/w.",
		},
		{
			name: "Updated speaks when queued",
			change: TaskChange{
				TaskID:  7,
				Kind:    TaskChangeUpdated,
				Actor:   Actor{Kind: ActorKindUser, Name: "alice"},
				Changed: []string{"name", "instructions"},
				Started: true,
				Runner:  "r",
			},
			wantMsg: "Task 7 queued: name, instructions.",
		},
		{
			name: "Updated silent when not queued",
			change: TaskChange{
				TaskID:  7,
				Kind:    TaskChangeUpdated,
				Actor:   Actor{Kind: ActorKindUser, Name: "alice"},
				Changed: []string{"name"},
			},
			wantMsg: "",
		},
		{
			// An edit to a task that already had queued runner work, with
			// Start NOT set — the gate must look at Started, not just Runner,
			// or we'd re-announce "queued" on every rename of a pending task.
			name: "Updated silent when queued but not started",
			change: TaskChange{
				TaskID:  7,
				Kind:    TaskChangeUpdated,
				Actor:   Actor{Kind: ActorKindUser, Name: "alice"},
				Changed: []string{"name"},
				Runner:  "r",
			},
			wantMsg: "",
		},
		{
			name: "Updated start-only when queued",
			change: TaskChange{
				TaskID:  7,
				Kind:    TaskChangeUpdated,
				Actor:   Actor{Kind: ActorKindUser, Name: "alice"},
				Started: true,
				Runner:  "r",
			},
			wantMsg: "Task 7 queued.",
		},
		{
			name: "Cancelled terminal speaks",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeCancelled,
				Actor:  Actor{Kind: ActorKindUser, Name: "alice"},
				Status: TaskStatusCancelled,
			},
			wantMsg: "Task 7 cancelled.",
		},
		{
			name: "Cancelled non-terminal silent (Cancelling)",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeCancelled,
				Actor:  Actor{Kind: ActorKindUser, Name: "alice"},
				Status: TaskStatusCancelling,
				Runner: "r",
			},
			wantMsg: "",
		},
		{
			name: "Restarted speaks",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeRestarted,
				Actor:  Actor{Kind: ActorKindUser, Name: "alice"},
				Runner: "r",
			},
			wantMsg: "Task 7 restart requested.",
		},
		{
			name: "Archived silent",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeArchived,
				Actor:  Actor{Kind: ActorKindUser, Name: "alice"},
			},
			wantMsg: "",
		},
		{
			name: "Unarchived silent",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeUnarchived,
				Actor:  Actor{Kind: ActorKindUser, Name: "alice"},
			},
			wantMsg: "",
		},
		{
			name: "AutoArchived silent",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeAutoArchived,
				Actor:  Actor{Kind: ActorKindArchiver},
			},
			wantMsg: "",
		},
		{
			name: "Woken speaks with description and URL",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeWoken,
				Actor:  Actor{Kind: ActorKindWebhook},
				Event: &Event{
					ID:          42,
					Description: "PR comment from alice",
					URL:         "https://github.com/x/y/pull/1#issuecomment-1",
				},
				Runner: "r",
			},
			wantMsg: "Task 7 woken by event 42: PR comment from alice (https://github.com/x/y/pull/1#issuecomment-1)",
		},
		{
			name: "ContainerStarted silent",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeContainerStarted,
				Actor:  Actor{Kind: ActorKindRunner},
				Status: TaskStatusRunning,
				Runner: "r",
			},
			wantMsg: "",
		},
		{
			name: "ContainerExited speaks on Completed",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeContainerExited,
				Actor:  Actor{Kind: ActorKindRunner},
				Status: TaskStatusCompleted,
			},
			wantMsg: "Task 7 completed.",
		},
		{
			name: "ContainerExited speaks on Cancelled",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeContainerExited,
				Actor:  Actor{Kind: ActorKindRunner},
				Status: TaskStatusCancelled,
			},
			wantMsg: "Task 7 cancelled.",
		},
		{
			name: "ContainerExited silent on re-queue (Pending)",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeContainerExited,
				Actor:  Actor{Kind: ActorKindRunner},
				Status: TaskStatusPending,
				Runner: "r",
			},
			wantMsg: "",
		},
		{
			name: "ContainerFailed speaks on Failed",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeContainerFailed,
				Actor:  Actor{Kind: ActorKindRunner},
				Status: TaskStatusFailed,
			},
			wantMsg: "Task 7 failed.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := tt.change
			c.Time = nonzeroTime
			got := c.Notification()
			assert.Equal(t, got.ChannelMessage, tt.wantMsg)
			// Agent-actionable rows must mention the task id so the channel
			// reader can correlate the line back to a task.
			if tt.wantMsg != "" {
				assert.Assert(t, strings.Contains(tt.wantMsg, "Task 7"), "expected task id in message: %q", tt.wantMsg)
			}
		})
	}
}

func TestTaskChange_Notification_AddressingFields(t *testing.T) {
	t.Parallel()
	// The caller/transport fields (OrgID/UserID/ClientID/Runner) flow from
	// the TaskChange directly into Notification, verbatim. The notification
	// addresses subscribers; it doesn't re-derive any of these.
	change := TaskChange{
		TaskID:    7,
		Kind:      TaskChangeCreated,
		Actor:     Actor{Kind: ActorKindUser, Name: "alice"},
		Workspace: "task-workspace",
		OrgID:     99,
		UserID:    "user-123",
		ClientID:  "client-abc",
		Runner:    "task-runner",
		Time:      nonzeroTime,
	}
	got := change.Notification()
	assert.Equal(t, got.OrgID, int64(99))
	assert.Equal(t, got.UserID, "user-123")
	assert.Equal(t, got.ClientID, "client-abc")
	assert.Equal(t, got.Runner, "task-runner")
	assert.Equal(t, got.Time, nonzeroTime)
	assert.Equal(t, got.Type, "change")
}

func TestTaskChange_Notification_Resources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change TaskChange
		want   []NotificationResource
	}{
		{
			name:   "Created",
			change: TaskChange{TaskID: 7, Kind: TaskChangeCreated},
			want: []NotificationResource{
				{Action: "created", Type: "task", ID: 7},
				{Action: "appended", Type: "task_logs", ID: 7},
			},
		},
		{
			name:   "Updated",
			change: TaskChange{TaskID: 7, Kind: TaskChangeUpdated},
			want: []NotificationResource{
				{Action: "updated", Type: "task", ID: 7},
				{Action: "appended", Type: "task_logs", ID: 7},
			},
		},
		{
			name:   "Cancelled",
			change: TaskChange{TaskID: 7, Kind: TaskChangeCancelled},
			want: []NotificationResource{
				{Action: "cancelled", Type: "task", ID: 7},
				{Action: "appended", Type: "task_logs", ID: 7},
			},
		},
		{
			name:   "Restarted",
			change: TaskChange{TaskID: 7, Kind: TaskChangeRestarted},
			want: []NotificationResource{
				{Action: "restarted", Type: "task", ID: 7},
				{Action: "appended", Type: "task_logs", ID: 7},
			},
		},
		{
			name:   "Archived",
			change: TaskChange{TaskID: 7, Kind: TaskChangeArchived},
			want: []NotificationResource{
				{Action: "archived", Type: "task", ID: 7},
				{Action: "appended", Type: "task_logs", ID: 7},
			},
		},
		{
			name:   "Unarchived",
			change: TaskChange{TaskID: 7, Kind: TaskChangeUnarchived},
			want: []NotificationResource{
				{Action: "unarchived", Type: "task", ID: 7},
				{Action: "appended", Type: "task_logs", ID: 7},
			},
		},
		{
			name:   "AutoArchived",
			change: TaskChange{TaskID: 7, Kind: TaskChangeAutoArchived},
			want: []NotificationResource{
				{Action: "archived", Type: "task", ID: 7},
				{Action: "appended", Type: "task_logs", ID: 7},
			},
		},
		{
			name: "Woken",
			change: TaskChange{
				TaskID: 7,
				Kind:   TaskChangeWoken,
				Event:  &Event{ID: 42},
			},
			want: []NotificationResource{
				{Action: "updated", Type: "task", ID: 7},
				{Action: "updated", Type: "event", ID: 42},
				{Action: "appended", Type: "task_logs", ID: 7},
			},
		},
		{
			name:   "ContainerStarted",
			change: TaskChange{TaskID: 7, Kind: TaskChangeContainerStarted},
			want: []NotificationResource{
				{Action: "updated", Type: "task", ID: 7},
				{Action: "appended", Type: "task_logs", ID: 7},
			},
		},
		{
			name:   "ContainerExited",
			change: TaskChange{TaskID: 7, Kind: TaskChangeContainerExited},
			want: []NotificationResource{
				{Action: "updated", Type: "task", ID: 7},
				{Action: "appended", Type: "task_logs", ID: 7},
			},
		},
		{
			name:   "ContainerFailed",
			change: TaskChange{TaskID: 7, Kind: TaskChangeContainerFailed},
			want: []NotificationResource{
				{Action: "updated", Type: "task", ID: 7},
				{Action: "appended", Type: "task_logs", ID: 7},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := tt.change
			got := c.Notification()
			assert.DeepEqual(t, got.Resources, tt.want, cmpopts.EquateEmpty())
		})
	}
}

func TestTaskChange_Updated_Accumulation(t *testing.T) {
	t.Parallel()
	// Accumulated change with Start=true: log and channel both derive from
	// the same Changed slice — the invariant PR #725 enforced ad-hoc, now a
	// structural property of the projection.
	change := TaskChange{
		TaskID:  7,
		Kind:    TaskChangeUpdated,
		Actor:   Actor{Kind: ActorKindUser, Name: "alice"},
		Changed: []string{"name", "instructions"},
		Started: true,
		Runner:  "r",
		Time:    nonzeroTime,
	}
	queued := change.Notification()
	assert.Equal(t, change.Log().Content, "alice updated task: name, instructions; started")
	assert.Equal(t, queued.ChannelMessage, "Task 7 queued: name, instructions.")

	// Same Changed without a runner (e.g., the task isn't queued):
	// the log row still fires, the channel stays silent.
	silentChange := change
	silentChange.Runner = ""
	silent := silentChange.Notification()
	assert.Equal(t, silent.ChannelMessage, "")
}
