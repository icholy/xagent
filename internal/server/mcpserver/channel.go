package mcpserver

import (
	"context"
	"strconv"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/mcpchannel"
)

// ChannelSender pushes a translated channel event. *mcpchannel.Transport
// satisfies it; an interface so the push logic is testable without a
// real transport.
type ChannelSender interface {
	SendChannel(ctx context.Context, p mcpchannel.Params) error
}

// ForwardNotification sends a notification to the mcp channel. It is a pure
// relay gated on n.ChannelMessage: an empty message means the publisher
// intentionally silenced the channel for this notification.
func ForwardNotification(ctx context.Context, sender ChannelSender, n model.Notification) error {
	if n.Type != "change" || n.ChannelMessage == "" {
		return nil
	}
	meta := map[string]string{}
	if id, ok := primaryTaskID(n.Resources); ok {
		meta["resource"] = "task"
		meta["id"] = strconv.FormatInt(id, 10)
	}
	return sender.SendChannel(ctx, mcpchannel.Params{
		Content: n.ChannelMessage,
		Meta:    meta,
	})
}

// primaryTaskID returns the ID of the first task resource in resources, or
// (0, false) if none is present.
func primaryTaskID(resources []model.NotificationResource) (int64, bool) {
	for _, r := range resources {
		if r.Type == "task" {
			return r.ID, true
		}
	}
	return 0, false
}
