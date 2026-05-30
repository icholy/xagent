package mcpserver

import (
	"context"

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
	if n.ChannelMessage == "" {
		return nil
	}
	return sender.SendChannel(ctx, mcpchannel.Params{
		Content: n.ChannelMessage,
	})
}
