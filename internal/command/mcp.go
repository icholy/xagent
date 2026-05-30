package command

import (
	"context"

	"github.com/icholy/xagent/internal/server/mcpserver"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
)

// McpCommand runs a local stdio MCP server that exposes the user-facing
// xagent tools by proxying calls to the remote C2 server's Connect RPC API.
// MCP clients (such as Claude Code) spawn it on the developer's machine.
var McpCommand = &cli.Command{
	Name:  "mcp",
	Usage: "Run a local MCP server that proxies to the C2 server",
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
		server := mcpserver.NewServer(client)
		return server.Run(ctx, &mcp.StdioTransport{})
	},
}
