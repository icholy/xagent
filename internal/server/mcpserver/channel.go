package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/x/mcpchannel"
	"github.com/icholy/xagent/internal/xagentclient"
)

// ChannelInstructions is the prompt fragment that tells Claude Code
// how to interpret the notifications/claude/channel events the bridge
// emits. Callers concatenate it with Instructions when building the
// server.
const ChannelInstructions = "Events from the xagent channel arrive as " +
	"<channel source=\"xagent\" resource=\"task\" status=... id=...> tags. " +
	"They notify you when an xagent task reaches a terminal status " +
	"(completed, failed, or cancelled)."

// ChannelSender pushes a translated channel event. *mcpchannel.Transport
// satisfies it; an interface so the push logic is testable without a
// real transport.
type ChannelSender interface {
	SendChannel(ctx context.Context, p mcpchannel.Params) error
}

// taskGetter is the subset of the xagent service NotificationChannel
// uses to look up task status. The Connect client and the in-process
// handler both satisfy it.
type taskGetter interface {
	GetTask(context.Context, *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error)
}

// NotificationChannel subscribes to the C2 server's per-org
// notification stream, filters task changes down to terminal-status
// transitions, and forwards each one as a notifications/claude/channel
// event on the supplied sender.
type NotificationChannel struct {
	sender    ChannelSender
	tasks     taskGetter
	serverURL string
	token     string
}

// NewNotificationChannel returns a NotificationChannel that pushes
// terminal task transitions on sender. service is used to look up the
// status of each task referenced by a notification.
func NewNotificationChannel(sender ChannelSender, service xagentv1connect.XAgentServiceHandler, serverURL, token string) *NotificationChannel {
	return &NotificationChannel{
		sender:    sender,
		tasks:     service,
		serverURL: serverURL,
		token:     token,
	}
}

// Run subscribes to the server's notification stream and dispatches
// events until ctx is done. Reconnect is owned by the underlying
// xagentclient.NotificationClient.
func (c *NotificationChannel) Run(ctx context.Context) error {
	nc := xagentclient.NewNotificationClient(xagentclient.NotificationClientOptions{
		BaseURL: c.serverURL,
		Token:   c.token,
		Handler: func(n model.Notification) { c.forward(ctx, n) },
	})
	err := nc.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// forward processes one notification, calling SendChannel once per
// task resource that has reached a terminal status. Non-change
// notifications, non-task resources, non-terminal tasks, GetTask
// errors, and SendChannel errors are all logged-and-skipped so one
// bad event doesn't drop the rest of the batch or the subscription.
func (c *NotificationChannel) forward(ctx context.Context, n model.Notification) {
	if n.Type != "change" {
		return
	}
	for _, r := range n.Resources {
		if r.Type != "task" {
			continue
		}
		status, ok := c.terminalStatus(ctx, r.ID)
		if !ok {
			continue
		}
		params := mcpchannel.Params{
			Content: fmt.Sprintf("task %d %s.", r.ID, status),
			Meta: map[string]string{
				"resource": "task",
				"status":   status,
				"id":       strconv.FormatInt(r.ID, 10),
			},
		}
		if err := c.sender.SendChannel(ctx, params); err != nil {
			slog.Warn("xagent channel: failed to send", "error", err)
		}
	}
}

// terminalStatus returns the lowercase status string ("completed",
// "failed", "cancelled") if the task is in a terminal status, and
// ("", false) otherwise. Errors looking up the task are logged and
// treated as non-terminal.
func (c *NotificationChannel) terminalStatus(ctx context.Context, id int64) (string, bool) {
	resp, err := c.tasks.GetTask(ctx, &xagentv1.GetTaskRequest{Id: id})
	if err != nil {
		slog.Warn("xagent channel: failed to fetch task", "id", id, "error", err)
		return "", false
	}
	switch resp.GetTask().GetStatus() {
	case xagentv1.TaskStatus_COMPLETED:
		return "completed", true
	case xagentv1.TaskStatus_FAILED:
		return "failed", true
	case xagentv1.TaskStatus_CANCELLED:
		return "cancelled", true
	default:
		return "", false
	}
}
