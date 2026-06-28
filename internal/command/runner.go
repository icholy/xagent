package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/icholy/xagent/internal/configfile"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/runner"
	"github.com/icholy/xagent/internal/runner/backend"
	dockerbackend "github.com/icholy/xagent/internal/runner/backend/docker"
	"github.com/icholy/xagent/internal/runner/backend/lambdamicrovm"
	"github.com/icholy/xagent/internal/runner/backend/lambdamicrovm/awsmvm"
	"github.com/icholy/xagent/internal/runner/taskstate"
	"github.com/icholy/xagent/internal/runner/workspace"
	"github.com/icholy/xagent/internal/x/awsmicrovm"
	"github.com/icholy/xagent/internal/x/common"
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
			Usage: "Fallback poll interval when SSE wake-ups are unavailable",
			Value: 30 * time.Second,
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
		&cli.StringFlag{
			Name:    "backend",
			Usage:   "Sandbox backend (docker, lambda-microvm)",
			Value:   "docker",
			Sources: cli.EnvVars("XAGENT_BACKEND"),
		},
		&cli.StringFlag{
			Name:    "state-dir",
			Usage:   "Directory for the runner-local task→sandbox-handle store",
			Value:   "/var/lib/xagent/tasks",
			Sources: cli.EnvVars("XAGENT_STATE_DIR"),
		},
		&cli.DurationFlag{
			Name:    "lambda-microvm-poll",
			Usage:   "Interval at which the lambda-microvm backend polls for MicroVM exits",
			Value:   10 * time.Second,
			Sources: cli.EnvVars("XAGENT_LAMBDA_MICROVM_POLL"),
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

		cfg, err := configfile.Load(&configfile.Overrides{
			Token: cmd.String("key"),
		})
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if cfg.Token == "" {
			return fmt.Errorf("not authenticated, run setup first or provide -key flag")
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

		backendName := cmd.String("backend")
		var be backend.Backend
		switch backendName {
		case "docker":
			be, err = dockerbackend.New(dockerbackend.Options{
				RunnerID: runnerID,
				Log:      log,
			})
			if err != nil {
				return err
			}
		case "lambda-microvm":
			awsCfg, err := config.LoadDefaultConfig(ctx)
			if err != nil {
				return fmt.Errorf("lambda-microvm backend: %w", err)
			}
			be, err = lambdamicrovm.New(lambdamicrovm.Options{
				Cloud:        awsmicrovm.NewClient(awsCfg),
				Stager:       awsmvm.NewS3Stager(awsCfg),
				RunnerID:     runnerID,
				Region:       awsCfg.Region,
				PollInterval: cmd.Duration("lambda-microvm-poll"),
				Log:          log,
			})
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown backend: %q", backendName)
		}

		// Open the runner-local task→sandbox-handle store, the single source of
		// truth for which sandbox belongs to which task.
		store, err := taskstate.Open(cmd.String("state-dir"))
		if err != nil {
			return fmt.Errorf("failed to open task store: %w", err)
		}

		r, err := runner.New(runner.Options{
			Client:      client,
			Backend:     be,
			Store:       store,
			ServerURL:   serverAddr,
			Workspaces:  workspaces,
			Concurrency: int(concurrency),
			RunnerID:    runnerID,
			Log:         log,
			Queue:       queue,
		})
		if err != nil {
			return err
		}
		defer r.Close()

		log.Info("runner started", "server", serverAddr, "config", configPath, "poll", pollInterval, "concurrency", concurrency, "backend", cmd.String("backend"))

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

		// Subscribe to server-sent task change notifications so the runner
		// reacts to new commands immediately instead of waiting for the
		// fallback poll.
		go func() {
			nc := xagentclient.NewNotificationClient(xagentclient.NotificationClientOptions{
				BaseURL: serverAddr,
				Runner:  runnerID,
				Token:   cfg.Token,
				Log:     log,
				Handler: func(model.Notification) { r.Wake() },
			})
			if err := nc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Error("notification client stopped", "error", err)
			}
		}()

		for {
			if err := r.Poll(ctx); err != nil {
				log.Error("failed to poll tasks", "error", err)
			}
			select {
			case <-r.WakeC():
			case <-time.After(pollInterval):
			case <-ctx.Done():
				return nil
			}
		}
	},
}
