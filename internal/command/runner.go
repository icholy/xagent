package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/icholy/xagent/internal/common"
	"github.com/icholy/xagent/internal/deviceauth"
	"github.com/icholy/xagent/internal/runner"
	"github.com/icholy/xagent/internal/workspace"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

func defaultRunnerID() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "default"
	}
	return hostname
}

var RunnerCommand = &cli.Command{
	Name:  "runner",
	Usage: "Start the runner (monitors tasks, manages containers)",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   xagentclient.DefaultURL,
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
		&cli.StringFlag{
			Name:    "token-file",
			Usage:   "Path to authentication token file",
			Value:   "data/token.json",
			Sources: cli.EnvVars("XAGENT_TOKEN_FILE"),
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
		&cli.StringFlag{
			Name:    "secret-file",
			Usage:   "Path to secret key file (auto-generated if missing)",
			Value:   "data/secret.key",
			Sources: cli.EnvVars("XAGENT_SECRET_FILE"),
		},
		&cli.IntFlag{
			Name:  "concurrency",
			Usage: "Maximum number of concurrent tasks (0 for unlimited)",
			Value: 0,
		},
		&cli.StringFlag{
			Name:  "id",
			Usage: "Unique identifier for this runner",
			Value: defaultRunnerID(),
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "Enable debug logging",
			Value: false,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		serverAddr := cmd.String("server")
		configPath := cmd.String("config")
		pollInterval := cmd.Duration("poll")
		prebuiltDir := cmd.String("prebuilt")
		secretFile := cmd.String("secret-file")
		concurrency := cmd.Int("concurrency")
		runnerID := cmd.String("id")
		debug := cmd.Bool("debug")

		// Create logger if debug is enabled
		log := slog.Default()
		if debug {
			opts := &slog.HandlerOptions{
				Level: slog.LevelDebug,
			}
			handler := slog.NewTextHandler(os.Stderr, opts)
			log = slog.New(handler)
		}

		auth, err := deviceauth.LoadToken(cmd.String("token-file"))
		if err != nil {
			return fmt.Errorf("failed to load token: %w", err)
		}

		workspaces, err := workspace.LoadConfig(configPath, nil)
		if err != nil {
			return fmt.Errorf("failed to load workspace config: %w", err)
		}

		r, err := runner.New(runner.Options{
			ServerURL:   serverAddr,
			PrebuiltDir: prebuiltDir,
			SecretFile:  secretFile,
			Workspaces:  workspaces,
			Concurrency: int(concurrency),
			RunnerID:    runnerID,
			Log:         log,
			Auth:        auth,
		})
		if err != nil {
			return err
		}
		defer r.Close()

		log.Info("runner started", "server", serverAddr, "config", configPath, "poll", pollInterval, "prebuilt", prebuiltDir, "concurrency", concurrency)

		// Register workspaces with the server (non-fatal if it fails)
		if err := r.RegisterWorkspaces(ctx); err != nil {
			log.Warn("failed to register workspaces", "error", err)
		}

		// Start container monitor in background
		go func() {
			for {
				err := r.Monitor(ctx)
				if errors.Is(err, context.Canceled) {
					break
				}
				log.Error("monitor error, restarting", "error", err)
				if !common.SleepContext(ctx, time.Second) {
					break
				}
			}
		}()

		// Reconcile any tasks that were running when the runner was stopped
		if err := r.Reconcile(ctx); err != nil {
			return fmt.Errorf("failed to reconcile: %w", err)
		}

		// Start autoprune goroutine
		go func() {
			for common.SleepContext(ctx, pollInterval) {
				if err := r.Prune(ctx); err != nil {
					log.Error("failed to prune containers", "error", err)
				}
			}
		}()

		for {
			if err := r.Poll(ctx); err != nil {
				log.Error("failed to poll tasks", "error", err)
			}
			if !common.SleepContext(ctx, pollInterval) {
				return nil
			}
		}
	},
}
