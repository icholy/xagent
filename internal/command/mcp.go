package command

import (
	"context"
	"net"
	"net/http"

	"github.com/icholy/xagent/internal/agentauth"
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
		&cli.BoolFlag{
			Name:  "proxy",
			Usage: "Proxy stdio to a streamable HTTP MCP server on the domain socket",
		},
		&cli.StringFlag{
			Name:  "socket",
			Usage: "Path to the Unix domain socket (used with --proxy)",
			Value: "/var/run/xagent.sock",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.Bool("proxy") {
			return runProxy(ctx, cmd)
		}
		return runServer(ctx, cmd)
	},
}

func runServer(ctx context.Context, cmd *cli.Command) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "xagent",
		Version: "1.0.0",
	}, nil)

	token := cmd.String("token")
	client := xagentclient.New(cmd.String("server"), agentauth.StaticTokenSource(token))
	task := &model.Task{
		ID:        cmd.Int64("task"),
		Runner:    cmd.String("runner"),
		Workspace: cmd.String("workspace"),
	}
	xmcp.NewServer(client, task).AddTools(server)

	return server.Run(ctx, &mcp.StdioTransport{})
}

func runProxy(ctx context.Context, cmd *cli.Command) error {
	socketPath := cmd.String("socket")
	token := cmd.String("token")
	remote := &mcp.StreamableClientTransport{
		Endpoint: "http://localhost/mcp",
		HTTPClient: &http.Client{
			Transport: &xagentclient.AuthTransport{
				Source: agentauth.StaticTokenSource(token),
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
						return net.Dial("unix", socketPath)
					},
				},
			},
		},
	}
	return xmcp.Proxy(ctx, &mcp.StdioTransport{}, remote)
}
