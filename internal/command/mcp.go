package command

import (
	"context"
	"fmt"
	"strconv"

	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/agentauth"
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
			Name:    "mode",
			Aliases: []string{"m"},
			Usage:   "MCP mode: external (for external agents) or container (for xagent-managed agents)",
			Value:   "external",
		},
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   xagentclient.DefaultURL,
		},
		&cli.Int64Flag{
			Name:    "task",
			Aliases: []string{"t"},
			Usage:   "Task ID (required for container mode)",
		},
		&cli.StringFlag{
			Name:    "runner",
			Aliases: []string{"r"},
			Usage:   "Runner ID (required for container mode)",
		},
		&cli.StringFlag{
			Name:    "workspace",
			Aliases: []string{"w"},
			Usage:   "Workspace name (required for container mode)",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		mode := cmd.String("mode")

		server := mcp.NewServer(&mcp.Implementation{
			Name:    "xagent",
			Version: "1.0.0",
		}, nil)

		switch mode {
		case "container":
			if !cmd.IsSet("task") {
				return fmt.Errorf("--task is required for container mode")
			}
			if !cmd.IsSet("runner") {
				return fmt.Errorf("--runner is required for container mode")
			}
			if !cmd.IsSet("workspace") {
				return fmt.Errorf("--workspace is required for container mode")
			}
			taskID := cmd.Int64("task")
			cfg, err := agent.LoadConfig(strconv.FormatInt(taskID, 10))
			if err != nil {
				return fmt.Errorf("failed to load agent config: %w", err)
			}
			if cfg.Token == "" {
				return fmt.Errorf("agent config missing token")
			}
			client := xagentclient.New(cmd.String("server"), agentauth.StaticTokenSource(cfg.Token))
			runner := cmd.String("runner")
			workspace := cmd.String("workspace")
			xmcp.NewServer(client, taskID, runner, workspace).AddTools(server)
		case "external":
			client := xagentclient.New(cmd.String("server"), nil)
			xmcp.NewExternalServer(client).AddTools(server)
		default:
			return fmt.Errorf("unknown mode: %s (must be 'container' or 'external')", mode)
		}

		return server.Run(ctx, &mcp.StdioTransport{})
	},
}
