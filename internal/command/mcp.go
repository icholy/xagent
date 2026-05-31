package command

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/mcpbridge"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/server/mcpserver"
	"github.com/icholy/xagent/internal/x/mcpchannel"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
)

// McpCommand runs a local stdio MCP bridge that re-exposes the
// user-facing xagent tools by proxying calls to the C2 server's
// Connect RPC API, and pushes task change notifications to the host
// Claude Code session as `notifications/claude/channel` events.
//
// The bridge declares the experimental `claude/channel` capability so
// Claude Code routes the notification stream into the session as
// `<channel>` tags. The push half is a translator on top of the
// existing per-org SSE notification stream — no new server endpoints
// or schemas are involved.
var McpCommand = &cli.Command{
	Name:  "mcp",
	Usage: "Run a stdio MCP server for managing tasks",
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
		&cli.BoolFlag{
			Name:  "channel",
			Usage: "Enable experimental claude/channel support",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		// One stable id per bridge process. It is sent on every mutation
		// RPC so the server stamps the resulting notifications with this
		// id, and on the SSE subscription so the notification client
		// filters those same events back out. Without it the bridge
		// would echo its own create_task/update_task changes back to the
		// host Claude Code session as channel events (#718).
		clientID := uuid.NewString()
		client := xagentclient.New(xagentclient.Options{
			BaseURL:  cmd.String("server"),
			Token:    cmd.String("token"),
			ClientID: clientID,
		})
		instructions := mcpserver.Instructions
		var capabilities mcp.ServerCapabilities
		if cmd.Bool("channel") {
			capabilities.Experimental = mcpchannel.Experimental()
			instructions += "\n\n" + mcpbridge.Instructions
		}
		server := mcp.NewServer(&mcp.Implementation{
			Name:    "xagent",
			Version: "1.0.0",
		}, &mcp.ServerOptions{
			Instructions: instructions,
			Capabilities: &capabilities,
		})
		mcpserver.AddTools(server, client)

		transport := mcpchannel.NewTransport(&mcp.StdioTransport{})

		// *mcpchannel.Transport satisfies mcpbridge.ChannelSender. When
		// channels are enabled the bridge owns the per-task subscription
		// set, the watch tools, and the mute-by-default forwarding gate;
		// the gate logic lives in internal/mcpbridge, not here.
		var ch *mcpbridge.Channel
		if cmd.Bool("channel") {
			ch = mcpbridge.NewChannel(transport)
			ch.AddTools(server)
		}

		session, err := server.Connect(ctx, transport, nil)
		if err != nil {
			return err
		}
		if ch != nil {
			go func() {
				nc := xagentclient.NewNotificationClient(xagentclient.NotificationClientOptions{
					BaseURL:  cmd.String("server"),
					Token:    cmd.String("token"),
					ClientID: clientID,
					Handler:  func(n model.Notification) { ch.Forward(ctx, n) },
				})
				if err := nc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
					slog.Warn("xagent channel: stream ended", "error", err)
				}
			}()
		}
		return session.Wait()
	},
}
