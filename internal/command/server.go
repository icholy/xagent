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
			Usage:   "Enable OIDC authentication",
			Sources: cli.EnvVars("XAGENT_AUTH_ENABLED"),
		},
		&cli.StringFlag{
			Name:    "oidc-issuer",
			Usage:   "OIDC issuer URL (e.g., https://your-instance.zitadel.cloud)",
			Sources: cli.EnvVars("XAGENT_OIDC_ISSUER"),
		},
		&cli.StringFlag{
			Name:    "oidc-client-id",
			Usage:   "OIDC client ID",
			Sources: cli.EnvVars("XAGENT_OIDC_CLIENT_ID"),
		},
		&cli.StringFlag{
			Name:    "oidc-client-secret",
			Usage:   "OIDC client secret",
			Sources: cli.EnvVars("XAGENT_OIDC_CLIENT_SECRET"),
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
			oidcIssuer := cmd.String("oidc-issuer")
			oidcClientID := cmd.String("oidc-client-id")
			oidcClientSecret := cmd.String("oidc-client-secret")

			if oidcIssuer == "" {
				return fmt.Errorf("--oidc-issuer is required when auth is enabled")
			}
			if oidcClientID == "" {
				return fmt.Errorf("--oidc-client-id is required when auth is enabled")
			}

			authConfig := &server.AuthConfig{
				IssuerURL:    oidcIssuer,
				ClientID:     oidcClientID,
				ClientSecret: oidcClientSecret,
			}

			auth, err := server.NewAuth(ctx, slog.Default(), authConfig, users)
			if err != nil {
				return fmt.Errorf("failed to initialize auth: %w", err)
			}
			opts.Auth = auth

			slog.Info("authentication enabled", "issuer", oidcIssuer)
		}

		srv := server.New(opts)

		slog.Info("starting server", "addr", addr, "db", dbPath, "auth", authEnabled, "pid", os.Getpid())
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	},
}
