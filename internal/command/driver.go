package command

import (
	"context"
	"io"
	"log/slog"
	"os"

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
			Usage:   "server URL",
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
		// Open the append-only in-sandbox log sink and tee the driver's slog
		// output into it alongside os.Stderr, so a completed run can be
		// inspected post-mortem via the reverse-shell. Opening is best-effort:
		// on failure the sink is a no-op and we log, but never fail the run.
		sink, err := agent.OpenLogSink(agent.DefaultLogPath)
		if err != nil {
			slog.Default().Warn("failed to open driver log sink, continuing without it",
				"path", agent.DefaultLogPath, "err", err)
		}
		defer sink.Close()

		log := slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, sink), nil))

		driver := &agent.Driver{
			TaskID: cmd.Int64("task"),
			Client: xagentclient.New(xagentclient.Options{
				BaseURL: cmd.String("server"),
				Token:   cmd.String("token"),
			}),
			Log:       log,
			LogSink:   sink,
			Config:    agent.DefaultConfigStore,
			ServerURL: cmd.String("server"),
			Token:     cmd.String("token"),
		}
		return driver.Run(ctx)
	},
}
