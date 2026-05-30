package command

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

// channelSender is the subset of *mcpchannel.Transport that
// pushTaskChannels needs. Defined as an interface so tests can
// exercise the SSE → channel translation without a real transport.
type channelSender interface {
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
func forwardNotification(ctx context.Context, transport channelSender, n model.Notification) {
	for _, params := range notificationToChannels(n) {
		if err := transport.SendChannel(ctx, params); err != nil {
			slog.Warn("xagent channel: failed to send", "error", err)
		}
	}
}

// pushTaskChannels subscribes to the C2 server's per-org SSE notification
// stream via xagentclient.NotificationClient and forwards task-relevant
// changes as notifications/claude/channel events on the given transport.
// Reconnect is owned by the client; this function returns when ctx is
// done.
func pushTaskChannels(ctx context.Context, transport channelSender, serverURL, token string) {
	nc := xagentclient.NewNotificationClient(xagentclient.NotificationClientOptions{
		BaseURL: serverURL,
		Token:   token,
		Handler: func(n model.Notification) { forwardNotification(ctx, transport, n) },
	})
	if err := nc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("xagent channel stream ended", "error", err)
	}
}
