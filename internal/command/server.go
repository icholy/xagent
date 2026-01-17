package command

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/icholy/xagent/internal/server"
	"github.com/icholy/xagent/internal/store"
	"github.com/urfave/cli/v3"
)

var ServerCommand = &cli.Command{
	Name:  "server",
	Usage: "Start the xagent server",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "addr",
			Aliases: []string{"a"},
			Usage:   "Address to listen on",
			Value:   ":6464",
		},
		&cli.StringFlag{
			Name:    "db",
			Aliases: []string{"d"},
			Usage:   "Database file path",
			Value:   "data/xagent.db",
		},
		&cli.BoolFlag{
			Name:  "notify",
			Usage: "Send system notification when a task finishes",
			Value: true,
		},
		&cli.BoolFlag{
			Name:    "auth-enabled",
			Usage:   "Enable Google OIDC authentication",
			Sources: cli.EnvVars("XAGENT_AUTH_ENABLED"),
		},
		&cli.StringFlag{
			Name:    "google-client-id",
			Usage:   "Google OAuth client ID",
			Sources: cli.EnvVars("XAGENT_GOOGLE_CLIENT_ID"),
		},
		&cli.StringFlag{
			Name:    "google-client-secret",
			Usage:   "Google OAuth client secret",
			Sources: cli.EnvVars("XAGENT_GOOGLE_CLIENT_SECRET"),
		},
		&cli.StringFlag{
			Name:    "base-url",
			Usage:   "Base URL for OAuth callbacks (e.g., http://localhost:6464)",
			Sources: cli.EnvVars("XAGENT_BASE_URL"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		addr := cmd.String("addr")
		dbPath := cmd.String("db")
		notifyFlag := cmd.Bool("notify")
		authEnabled := cmd.Bool("auth-enabled")

		db, err := store.Open(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		tasks := store.NewTaskRepository(db)
		logs := store.NewLogRepository(db)
		links := store.NewLinkRepository(db)
		events := store.NewEventRepository(db)
		workspaces := store.NewWorkspaceRepository(db)
		users := store.NewUserRepository(db)

		opts := server.Options{
			Tasks:      tasks,
			Logs:       logs,
			Links:      links,
			Events:     events,
			Workspaces: workspaces,
			Notify:     notifyFlag,
		}

		if authEnabled {
			googleClientID := cmd.String("google-client-id")
			googleClientSecret := cmd.String("google-client-secret")
			baseURL := cmd.String("base-url")

			if googleClientID == "" {
				return fmt.Errorf("--google-client-id is required when auth is enabled")
			}
			if googleClientSecret == "" {
				return fmt.Errorf("--google-client-secret is required when auth is enabled")
			}
			if baseURL == "" {
				return fmt.Errorf("--base-url is required when auth is enabled")
			}

			authConfig := &server.AuthConfig{
				GoogleClientID:     googleClientID,
				GoogleClientSecret: googleClientSecret,
				BaseURL:            baseURL,
			}

			auth, err := server.NewAuth(ctx, slog.Default(), authConfig, users)
			if err != nil {
				return fmt.Errorf("failed to initialize auth: %w", err)
			}
			opts.Auth = auth

			slog.Info("authentication enabled", "base_url", baseURL)
		}

		srv := server.New(opts)

		slog.Info("starting server", "addr", addr, "db", dbPath, "auth", authEnabled, "pid", os.Getpid())
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	},
}
