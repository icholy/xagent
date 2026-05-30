package command

import (
	"context"

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
	Usage: "Run a local MCP bridge: proxies xagent tools and pushes task changes as Claude Code channel events",
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
			Required: true,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		client := xagentclient.New(xagentclient.Options{
			BaseURL: cmd.String("server"),
			Token:   cmd.String("token"),
		})
		server := mcp.NewServer(&mcp.Implementation{
			Name:    "xagent",
			Version: "1.0.0",
		}, &mcp.ServerOptions{
			Instructions: mcpserver.Instructions + "\n\n" + mcpserver.ChannelInstructions,
			Capabilities: &mcp.ServerCapabilities{
				Experimental: mcpchannel.Experimental(),
			},
		})
		mcpserver.AddTools(server, client)

		transport := mcpchannel.NewTransport(&mcp.StdioTransport{})
		session, err := server.Connect(ctx, transport, nil)
		if err != nil {
			return err
		}
		go mcpserver.PushChannels(ctx, transport, cmd.String("server"), cmd.String("token"))
		return session.Wait()
	},
}
