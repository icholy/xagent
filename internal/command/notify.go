package command

import (
	"context"
	"errors"
	"log/slog"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/notify"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

// NotifyCommand runs a long-lived daemon that subscribes to the server's
// notification stream and emits a system notification for each change that
// carries a human-readable message.
//
// It reuses the same per-org SSE stream as the runner and the mcp channel
// bridge. Notifications are gated on the ChannelMessage field: an empty
// message is silent, mirroring the channel forwarder in the mcp command.
var NotifyCommand = &cli.Command{
	Name:  "notify",
	Usage: "Subscribe to server notifications and emit system notifications",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   xagentclient.DefaultURL,
		},
		&cli.StringFlag{
			Name:     "token",
			Usage:    "Authentication token",
			Sources:  cli.EnvVars("XAGENT_TOKEN"),
			Required: true,
		},
		&cli.StringFlag{
			Name:  "title",
			Usage: "Title shown on each system notification",
			Value: "xagent",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		title := cmd.String("title")
		nc := xagentclient.NewNotificationClient(xagentclient.NotificationClientOptions{
			BaseURL: cmd.String("server"),
			Token:   cmd.String("token"),
			Handler: func(n model.Notification) {
				// ChannelMessage holds the human-readable line; empty
				// means the change is silent (e.g. the "ready" event or
				// internal-only changes), so there's nothing to show.
				if n.ChannelMessage == "" {
					return
				}
				if err := notify.Send(title, n.ChannelMessage); err != nil {
					slog.Warn("failed to send system notification", "error", err)
				}
			},
		})
		if err := nc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	},
}
