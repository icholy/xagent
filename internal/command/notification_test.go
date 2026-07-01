package command

import (
	"strings"
	"testing"

	"github.com/icholy/xagent/internal/model"
	"gotest.tools/v3/assert"
)

// lifecycleNote builds a notification carrying a lifecycle event of the given
// kind/to_status, with the given for_runner, for the forward-policy tests.
func lifecycleNote(kind model.LifecycleKind, toStatus model.TaskStatus, runner string) model.Notification {
	return model.Notification{
		Runner: runner,
		TaskEvent: &model.Event{
			TaskID: 7,
			Payload: &model.LifecyclePayload{
				Kind:     kind,
				ToStatus: toStatus.Label(),
			},
		},
	}
}

func TestChannelMessage_ForwardPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		n       model.Notification
		forward bool
		want    string // substring expected in the rendered line when forwarded
	}{
		{
			name: "external always forwards",
			n: model.Notification{TaskEvent: &model.Event{
				TaskID:  7,
				Payload: &model.ExternalPayload{Description: "PR comment", URL: "https://x/1"},
			}},
			forward: true,
			want:    "Task 7: PR comment (https://x/1)",
		},
		{
			name:    "created forwards",
			n:       lifecycleNote(model.LifecycleKindCreated, model.TaskStatusPending, "r"),
			forward: true,
			want:    "Created",
		},
		{
			name:    "restarted forwards",
			n:       lifecycleNote(model.LifecycleKindRestarted, model.TaskStatusPending, "r"),
			forward: true,
		},
		{
			name:    "archived forwards",
			n:       lifecycleNote(model.LifecycleKindArchived, model.TaskStatusCompleted, ""),
			forward: true,
		},
		{
			name:    "updated queued forwards",
			n:       lifecycleNote(model.LifecycleKindUpdated, model.TaskStatusPending, "r"),
			forward: true,
		},
		{
			name:    "updated name-only suppressed",
			n:       lifecycleNote(model.LifecycleKindUpdated, model.TaskStatusRunning, ""),
			forward: false,
		},
		{
			name:    "cancelled terminal forwards",
			n:       lifecycleNote(model.LifecycleKindCancelled, model.TaskStatusCancelled, ""),
			forward: true,
		},
		{
			name:    "cancelling non-terminal suppressed",
			n:       lifecycleNote(model.LifecycleKindCancelled, model.TaskStatusCancelling, ""),
			forward: false,
		},
		{
			name:    "sandbox exited terminal forwards",
			n:       lifecycleNote(model.LifecycleKindSandboxExited, model.TaskStatusCompleted, ""),
			forward: true,
		},
		{
			name:    "sandbox exited non-terminal suppressed",
			n:       lifecycleNote(model.LifecycleKindSandboxExited, model.TaskStatusRunning, ""),
			forward: false,
		},
		{
			name:    "sandbox failed forwards",
			n:       lifecycleNote(model.LifecycleKindSandboxFailed, model.TaskStatusFailed, ""),
			forward: true,
		},
		{
			name:    "sandbox started suppressed",
			n:       lifecycleNote(model.LifecycleKindSandboxStarted, model.TaskStatusRunning, ""),
			forward: false,
		},
		{
			name:    "unarchived suppressed",
			n:       lifecycleNote(model.LifecycleKindUnarchived, model.TaskStatusCompleted, ""),
			forward: false,
		},
		{
			name:    "auto-archived suppressed",
			n:       lifecycleNote(model.LifecycleKindAutoArchived, model.TaskStatusCompleted, ""),
			forward: false,
		},
		{
			name:    "nil event suppressed",
			n:       model.Notification{},
			forward: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg, ok := channelMessage(tt.n)
			assert.Equal(t, ok, tt.forward)
			if !tt.forward {
				assert.Equal(t, msg, "")
				return
			}
			assert.Assert(t, msg != "", "expected a rendered line")
			if tt.want != "" {
				assert.Assert(t, strings.Contains(msg, tt.want), "got %q, want substring %q", msg, tt.want)
			}
		})
	}
}

// TestLifecycleOf_Terminal mirrors the notify daemon's gate: only a terminal
// lifecycle event is surfaced.
func TestLifecycleOf_Terminal(t *testing.T) {
	t.Parallel()

	// A terminal sandbox exit surfaces.
	n := lifecycleNote(model.LifecycleKindSandboxExited, model.TaskStatusCompleted, "")
	lc, ok := lifecycleOf(n.TaskEvent)
	assert.Assert(t, ok)
	assert.Assert(t, lc.IsTerminal())

	// A direct Pending->Cancelled cancel is terminal and now surfaces — the
	// behavior change from the old TaskStatus gate, which stayed silent on it.
	n = lifecycleNote(model.LifecycleKindCancelled, model.TaskStatusCancelled, "")
	lc, ok = lifecycleOf(n.TaskEvent)
	assert.Assert(t, ok)
	assert.Assert(t, lc.IsTerminal())

	// A non-lifecycle event is not surfaced.
	_, ok = lifecycleOf(&model.Event{Payload: &model.ExternalPayload{}})
	assert.Assert(t, !ok)

	// A nil event is not surfaced.
	_, ok = lifecycleOf(nil)
	assert.Assert(t, !ok)
}
