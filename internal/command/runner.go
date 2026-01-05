package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/icholy/xagent/internal/runner"
	"github.com/icholy/xagent/internal/workspace"
	"github.com/urfave/cli/v3"
)

var RunnerCommand = &cli.Command{
	Name:  "runner",
	Usage: "Start the runner (monitors tasks, manages containers)",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   "http://localhost:8080",
		},
		&cli.StringFlag{
			Name:    "config",
			Aliases: []string{"c"},
			Usage:   "Workspace config file",
			Value:   "workspaces.yaml",
		},
		&cli.DurationFlag{
			Name:  "poll",
			Usage: "Poll interval for pending tasks",
			Value: 5 * time.Second,
		},
		&cli.StringFlag{
			Name:  "prebuilt",
			Usage: "Directory containing prebuilt xagent binaries",
			Value: "prebuilt",
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "Stream container logs to stdout/stderr",
		},
		&cli.IntFlag{
			Name:  "concurrency",
			Usage: "Maximum number of concurrent tasks (0 for unlimited)",
			Value: 0,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		serverAddr := cmd.String("server")
		configPath := cmd.String("config")
		pollInterval := cmd.Duration("poll")
		prebuiltDir := cmd.String("prebuilt")
		debug := cmd.Bool("debug")
		concurrency := cmd.Int("concurrency")

		workspaces, err := workspace.LoadConfig(configPath, nil)
		if err != nil {
			return fmt.Errorf("failed to load workspace config: %w", err)
		}

		r, err := runner.New(runner.Options{
			ServerURL:   serverAddr,
			PrebuiltDir: prebuiltDir,
			Workspaces:  workspaces,
			Debug:       debug,
			Concurrency: int(concurrency),
		})
		if err != nil {
			return err
		}
		defer r.Close()

		slog.Info("runner started", "server", serverAddr, "config", configPath, "poll", pollInterval, "prebuilt", prebuiltDir, "concurrency", concurrency)

		// Start container monitor in background
		go func() {
			for {
				err := r.Monitor(ctx)
				if errors.Is(err, context.Canceled) {
					break
				}
				slog.Error("monitor error, restarting", "error", err)
				time.Sleep(time.Second)
			}
		}()

		// Reconcile any tasks that were running when the runner was stopped
		if err := r.Reconcile(ctx); err != nil {
			slog.Error("failed to reconcile", "error", err)
		}

		for {
			if err := r.Poll(ctx); err != nil {
				slog.Error("failed to poll tasks", "error", err)
			}
			time.Sleep(pollInterval)
		}
	},
}
