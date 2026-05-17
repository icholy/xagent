package command

import (
	"context"
	"log/slog"

	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var DriverCommand = &cli.Command{
	Name:  "driver",
	Usage: "Run an agent for a task",
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
			Usage:    "Task ID to execute",
			Required: true,
		},
		&cli.StringFlag{
			Name:  "token",
			Usage: "Authentication token for the agent",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		driver := &agent.Driver{
			TaskID: cmd.Int64("task"),
			Client: xagentclient.New(xagentclient.Options{
				BaseURL: cmd.String("server"),
				Token:   cmd.String("token"),
			}),
			Log: slog.Default(),
		}
		return driver.Run(ctx)
	},
}
