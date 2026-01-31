package command

import (
	"context"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/icholy/xagent/internal/xmcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
)

var McpCommand = &cli.Command{
	Name:  "mcp",
	Usage: "Run an MCP server that provides xagent tools",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   xagentclient.DefaultURL,
		},
		&cli.Int64Flag{
			Name:     "task",
			Aliases:  []string{"t"},
			Usage:    "Task ID",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "runner",
			Aliases:  []string{"r"},
			Usage:    "Runner ID",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "workspace",
			Aliases:  []string{"w"},
			Usage:    "Workspace name",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "token",
			Usage:    "Authentication token",
			Required: true,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		server := mcp.NewServer(&mcp.Implementation{
			Name:    "xagent",
			Version: "1.0.0",
		}, nil)

		client := xagentclient.New(xagentclient.Options{BaseURL: cmd.String("server"), Token: cmd.String("token")})
		task := &model.Task{
			ID:        cmd.Int64("task"),
			Runner:    cmd.String("runner"),
			Workspace: cmd.String("workspace"),
		}
		xmcp.NewServer(client, task).AddTools(server)

		return server.Run(ctx, &mcp.StdioTransport{})
	},
}
