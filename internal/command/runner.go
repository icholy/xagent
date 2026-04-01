package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/icholy/xagent/internal/agentauth"
	"github.com/icholy/xagent/internal/common"
	"github.com/icholy/xagent/internal/configfile"
	"github.com/icholy/xagent/internal/runner"
	"github.com/icholy/xagent/internal/workspace"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

func defaultWorkspacesPath() string {
	path, err := workspace.DefaultPath()
	if err != nil {
		return "workspaces.yaml"
	}
	return path
}

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
			Name:    "workspaces",
			Aliases: []string{"w"},
			Usage:   "Workspace config file",
			Value:   defaultWorkspacesPath(),
		},
		&cli.DurationFlag{
			Name:  "poll",
			Usage: "Poll interval for pending tasks",
			Value: 5 * time.Second,
		},
		&cli.IntFlag{
			Name:  "concurrency",
			Usage: "Maximum number of concurrent tasks (0 for unlimited)",
			Value: 5,
		},
		&cli.StringFlag{
			Name:    "id",
			Usage:   "Unique identifier for this runner (no spaces or special characters)",
			Value:   defaultRunnerID(),
			Sources: cli.EnvVars("XAGENT_RUNNER_ID"),
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "Enable debug logging",
			Value: false,
		},
		&cli.StringFlag{
			Name:    "key",
			Aliases: []string{"k"},
			Usage:   "API key (takes priority over config file)",
			Sources: cli.EnvVars("XAGENT_API_KEY"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		serverAddr := cmd.String("server")
		configPath := cmd.String("workspaces")
		pollInterval := cmd.Duration("poll")
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

		cfg, err := configfile.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if cmd.IsSet("key") {
			cfg.Token = cmd.String("key")
		}
		if cfg.Token == "" {
			return fmt.Errorf("not authenticated, run setup first or provide -key flag")
		}
		if envKey := os.Getenv("XAGENT_PRIVATE_KEY"); envKey != "" {
			key, err := configfile.DecodePrivateKey([]byte(envKey))
			if err != nil {
				return fmt.Errorf("failed to decode XAGENT_PRIVATE_KEY: %w", err)
			}
			cfg.PrivateKey = key
		} else if cfg.PrivateKey == nil {
			key, err := agentauth.CreatePrivateKey()
			if err != nil {
				return fmt.Errorf("failed to generate private key: %w", err)
			}
			cfg.PrivateKey = key
			if err := configfile.Save(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
		}

		workspaces, err := workspace.LoadConfig(configPath, nil)
		if err != nil {
			return fmt.Errorf("failed to load workspace config: %w", err)
		}

		client := xagentclient.New(xagentclient.Options{
			BaseURL: serverAddr,
			Token:   cfg.Token,
		})

		queue := runner.NewEventQueue(runner.EventQueueOptions{
			Client:        client,
			Log:           log,
			RetryInterval: pollInterval,
		})

		r, err := runner.New(runner.Options{
			Client:      client,
			PrivateKey:  cfg.PrivateKey,
			Workspaces:  workspaces,
			Concurrency: int(concurrency),
			RunnerID:    runnerID,
			Log:         log,
			SocketPath:  filepath.Join(os.TempDir(), "xagent", runnerID+".sock"),
			Queue:       queue,
		})
		if err != nil {
			return err
		}
		defer r.Close()

		log.Info("runner started", "server", serverAddr, "config", configPath, "poll", pollInterval, "concurrency", concurrency)

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

		// Start event queue drain goroutine
		go queue.Run(ctx)

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
