package command

import (
	"context"
	"log/slog"
	"os"

	"github.com/icholy/xagent/internal/githubmcp"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var GitHubMCPCommand = &cli.Command{
	Name:  "github-mcp",
	Usage: "Front the GitHub MCP server with rotating GitHub App installation tokens",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Usage:   "C2 server URL",
			Value:   xagentclient.DefaultURL,
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
		&cli.StringFlag{
			Name:    "token",
			Usage:   "Authentication token",
			Sources: cli.EnvVars("XAGENT_TOKEN"),
		},
		&cli.StringFlag{
			Name:  "url",
			Usage: "Upstream GitHub MCP endpoint",
			Value: githubmcp.DefaultURL,
		},
		&cli.DurationFlag{
			Name:  "refresh-margin",
			Usage: "How long before expiry to rotate the upstream session",
			Value: githubmcp.DefaultRefreshMargin,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		return githubmcp.Run(ctx, githubmcp.Config{
			Client: xagentclient.New(xagentclient.Options{
				BaseURL: cmd.String("server"),
				Token:   cmd.String("token"),
			}),
			URL:           cmd.String("url"),
			RefreshMargin: cmd.Duration("refresh-margin"),
			Logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
		})
	},
}
