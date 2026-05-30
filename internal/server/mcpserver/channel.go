package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/mcpchannel"
	"github.com/icholy/xagent/internal/xagentclient"
)

// ChannelInstructions is the prompt fragment that tells Claude Code
// how to interpret the notifications/claude/channel events the
// bridge emits. Callers concatenate it with Instructions when
// building the server.
const ChannelInstructions = "Events from the xagent channel arrive as " +
	"<channel source=\"xagent\" action=... resource=... id=...> tags. " +
	"They notify you that an xagent task, log, link, or event changed. " +
	"Call get_task with the id for details before acting."

// ChannelSender pushes a translated channel event. *mcpchannel.Transport
// satisfies it; an interface so the push logic is testable without a
// real transport.
type ChannelSender interface {
	SendChannel(ctx context.Context, p mcpchannel.Params) error
}

// channelResourceTypes is the allowlist of model.NotificationResource
// types that the bridge forwards to Claude as channel events. Anything
// else (e.g. future resource types unrelated to task work) is dropped.
var channelResourceTypes = map[string]bool{
	"task":      true,
	"event":     true,
	"log":       true,
	"link":      true,
	"task_logs": true,
}

// notificationToChannels translates a model.Notification into zero or more
// channel notification payloads. "ready" notifications and resources whose
// type is not in the allowlist are dropped.
func notificationToChannels(n model.Notification) []mcpchannel.Params {
	if n.Type != "change" {
		return nil
	}
	var out []mcpchannel.Params
	for _, r := range n.Resources {
		if !channelResourceTypes[r.Type] {
			continue
		}
		out = append(out, mcpchannel.Params{
			Content: fmt.Sprintf("%s %d was %s.", r.Type, r.ID, r.Action),
			Meta: map[string]string{
				"action":   r.Action,
				"resource": r.Type,
				"id":       strconv.FormatInt(r.ID, 10),
			},
		})
	}
	return out
}

// forwardNotification translates n and sends each resulting channel
// event. A SendChannel failure is logged and skipped so one bad send
// doesn't drop the rest of the batch or the subscription.
func forwardNotification(ctx context.Context, sender ChannelSender, n model.Notification) {
	for _, params := range notificationToChannels(n) {
		if err := sender.SendChannel(ctx, params); err != nil {
			slog.Warn("xagent channel: failed to send", "error", err)
		}
	}
}

// PushChannels subscribes to the C2 server's per-org notification
// stream and forwards task-relevant changes as
// notifications/claude/channel events on sender. Reconnect is owned
// by the underlying xagentclient.NotificationClient; this function
// returns when ctx is done.
func PushChannels(ctx context.Context, sender ChannelSender, serverURL, token string) {
	nc := xagentclient.NewNotificationClient(xagentclient.NotificationClientOptions{
		BaseURL: serverURL,
		Token:   token,
		Handler: func(n model.Notification) { forwardNotification(ctx, sender, n) },
	})
	if err := nc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("xagent channel stream ended", "error", err)
	}
}
