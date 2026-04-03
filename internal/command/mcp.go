package command

import (
	"context"
	"time"

	"github.com/icholy/xagent/internal/agentmcp"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/xagentclient"
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
		&cli.BoolFlag{
			Name:  "channel",
			Usage: "Enable Claude Code channel support",
		},
		&cli.DurationFlag{
			Name:  "channel-poll-interval",
			Usage: "Channel event polling interval",
			Value: 3 * time.Second,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		channelEnabled := cmd.Bool("channel")

		opts := &mcp.ServerOptions{}
		if channelEnabled {
			opts.Capabilities = &mcp.ServerCapabilities{
				Experimental: map[string]any{
					"claude/channel": map[string]any{},
				},
			}
			opts.Instructions = agentmcp.ChannelInstructions
		}

		server := mcp.NewServer(&mcp.Implementation{
			Name:    "xagent",
			Version: "1.0.0",
		}, opts)

		client := xagentclient.New(xagentclient.Options{BaseURL: cmd.String("server"), Token: cmd.String("token")})
		task := &model.Task{
			ID:        cmd.Int64("task"),
			Runner:    cmd.String("runner"),
			Workspace: cmd.String("workspace"),
		}
		xmcp := agentmcp.NewServer(client, task)
		xmcp.AddTools(server)

		if channelEnabled {
			transport, notifier := agentmcp.WrapTransport(&mcp.StdioTransport{})
			session, err := server.Connect(ctx, transport, nil)
			if err != nil {
				return err
			}
			go xmcp.PollEvents(ctx, notifier, cmd.Duration("channel-poll-interval"))
			return session.Wait()
		}

		return server.Run(ctx, &mcp.StdioTransport{})
	},
}
