package command

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
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
		&cli.DurationFlag{
			Name:  "auto-archive",
			Usage: "Default auto-archive delay for tasks created via create_task when the call omits auto_archive. 0 = never, negative = archive immediately on terminal status, positive = delay (e.g. 1h, 24h). The per-call auto_archive param overrides this.",
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
		var capabilities mcp.ServerCapabilities
		if cmd.Bool("channel") {
			capabilities.Experimental = mcpchannel.Experimental()
		}
		server := mcp.NewServer(&mcp.Implementation{
			Name:    "xagent",
			Version: "1.0.0",
		}, &mcp.ServerOptions{
			Instructions: mcpserver.Instructions,
			Capabilities: &capabilities,
		})
		var toolOpts []mcpserver.Option
		if cmd.IsSet("auto-archive") {
			toolOpts = append(toolOpts, mcpserver.WithDefaultAutoArchive(cmd.Duration("auto-archive")))
		}
		mcpserver.AddTools(server, client, toolOpts...)

		transport := mcpchannel.NewTransport(&mcp.StdioTransport{})
		session, err := server.Connect(ctx, transport, nil)
		if err != nil {
			return err
		}
		if cmd.Bool("channel") {
			go func() {
				nc := xagentclient.NewNotificationClient(xagentclient.NotificationClientOptions{
					BaseURL:  cmd.String("server"),
					Token:    cmd.String("token"),
					ClientID: clientID,
					Handler: func(n model.Notification) {
						msg, ok := channelMessage(n)
						if !ok {
							return
						}
						if err := transport.SendChannel(ctx, mcpchannel.Params{Content: msg}); err != nil {
							slog.Warn("xagent channel: failed to send", "error", err)
						}
					},
				})
				if err := nc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
					slog.Warn("xagent channel: stream ended", "error", err)
				}
			}()
		}
		return session.Wait()
	},
}
