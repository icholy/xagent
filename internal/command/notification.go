package command

import (
	"fmt"

	"github.com/icholy/xagent/internal/model"
)

// lifecycleOf returns the lifecycle payload carried by a notification's causal
// event, or (nil, false) when the event is absent or of another kind. It is the
// nil-safe accessor the consumers gate on instead of reaching into
// Notification.TaskEvent.Payload directly.
func lifecycleOf(e *model.Event) (*model.LifecyclePayload, bool) {
	if e == nil {
		return nil, false
	}
	p, ok := e.Payload.(*model.LifecyclePayload)
	return p, ok
}

// channelMessage applies the mcp channel-bridge forward policy to a
// notification: it decides whether the notification's causal task event should
// reach the agent channel and renders the line to forward. This is the single
// place the policy lives.
//
// It forwards:
//   - external events (webhook triggers) as the wake line, always;
//   - Created, Restarted, and Archived lifecycle events;
//   - Updated lifecycle events only when the update queued runner work
//     (n.Runner != ""), i.e. a start/re-queue rather than a bare rename;
//   - Cancelled, SandboxExited, and SandboxFailed lifecycle events only when
//     they land on a terminal outcome (LifecyclePayload.IsTerminal).
//
// It suppresses everything else — Unarchived, AutoArchived, SandboxStarted,
// non-terminal Cancelled/SandboxExited, name-only Updated, and a nil event.
func channelMessage(n model.Notification) (string, bool) {
	e := n.TaskEvent
	if e == nil {
		return "", false
	}
	switch p := e.Payload.(type) {
	case *model.ExternalPayload:
		return fmt.Sprintf("Task %d: %s", e.TaskID, p.Summary()), true
	case *model.LifecyclePayload:
		if !forwardLifecycle(p, n.Runner) {
			return "", false
		}
		return fmt.Sprintf("Task %d: %s", e.TaskID, p.Summary()), true
	default:
		return "", false
	}
}

// forwardLifecycle reports whether a lifecycle event should be forwarded to the
// agent channel. runner is the notification's for_runner field, used to gate
// Updated to the queued case.
func forwardLifecycle(p *model.LifecyclePayload, runner string) bool {
	switch p.Kind {
	case model.LifecycleKindCreated,
		model.LifecycleKindRestarted,
		model.LifecycleKindArchived:
		return true
	case model.LifecycleKindUpdated:
		// Only when the update queued the task (there is pending runner work);
		// a bare rename stays silent.
		return runner != ""
	case model.LifecycleKindCancelled,
		model.LifecycleKindSandboxExited,
		model.LifecycleKindSandboxFailed:
		// Only terminal run outcomes.
		return p.IsTerminal()
	default:
		// Unarchived, AutoArchived, SandboxStarted, and anything else.
		return false
	}
}
