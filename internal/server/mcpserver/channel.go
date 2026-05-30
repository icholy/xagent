package mcpserver

import (
	"context"
	"errors"
	"fmt"
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

// ForwardNotification sends notifications to the mcp channel
func ForwardNotification(ctx context.Context, sender ChannelSender, n model.Notification) error {
	if n.Type != "change" {
		return nil
	}
	var errs []error
	for _, r := range n.Resources {
		if r.Type != "task" {
			continue
		}
		errs = append(errs, sender.SendChannel(ctx, mcpchannel.Params{
			Content: fmt.Sprintf("task %d was %s.", r.ID, r.Action),
			Meta: map[string]string{
				"resource": "task",
				"id":       strconv.FormatInt(r.ID, 10),
			},
		}))
	}
	return errors.Join(errs...)
}
