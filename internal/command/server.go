package command

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/deviceauth"
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
			Usage:   "PostgreSQL connection string",
			Sources: cli.EnvVars("DATABASE_URL"),
		},
		&cli.BoolFlag{
			Name:  "notify",
			Usage: "Send system notification when a task finishes",
			Value: true,
		},
		&cli.StringFlag{
			Name:    "auth-domain",
			Usage:   "ZITADEL domain (e.g. instance.zitadel.cloud)",
			Sources: cli.EnvVars("XAGENT_AUTH_DOMAIN"),
		},
		&cli.StringFlag{
			Name:    "auth-client-id",
			Usage:   "ZITADEL client ID",
			Sources: cli.EnvVars("XAGENT_AUTH_CLIENT_ID"),
		},
		&cli.StringFlag{
			Name:    "auth-client-secret",
			Usage:   "ZITADEL client secret",
			Sources: cli.EnvVars("XAGENT_AUTH_CLIENT_SECRET"),
		},
		&cli.StringFlag{
			Name:    "base-url",
			Usage:   "Base URL for the server (e.g. https://xagent.example.com)",
			Sources: cli.EnvVars("XAGENT_BASE_URL"),
		},
		&cli.StringFlag{
			Name:    "auth-device-client-id",
			Usage:   "ZITADEL client ID for device flow (native app)",
			Sources: cli.EnvVars("XAGENT_AUTH_DEVICE_CLIENT_ID"),
		},
		&cli.StringFlag{
			Name:    "auth-encryption-key",
			Usage:   "Hex-encoded 32-byte key for session encryption (generated if not set)",
			Sources: cli.EnvVars("XAGENT_AUTH_ENCRYPTION_KEY"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		addr := cmd.String("addr")
		dbPath := cmd.String("db")
		notifyFlag := cmd.Bool("notify")

		db, err := store.Open(dbPath, true)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		st := store.New(db)

		domain := cmd.String("auth-domain")
		baseURL := cmd.String("base-url")
		key, err := apiauth.DecodeEncryptionKey(cmd.String("auth-encryption-key"))
		if err != nil {
			return fmt.Errorf("invalid encryption key: %w", err)
		}
		auth, err := apiauth.New(ctx, apiauth.Config{
			Domain:        domain,
			ClientID:      cmd.String("auth-client-id"),
			ClientSecret:  cmd.String("auth-client-secret"),
			RedirectURI:   baseURL + "/auth/callback",
			PostLogoutURI: baseURL + "/",
			EncryptionKey: key,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize auth: %w", err)
		}

		srv := server.New(server.Options{
			Store:  st,
			Notify: notifyFlag,
			Auth:   auth,
			Discovery: deviceauth.DiscoveryConfig{
				DeviceAuthorizationEndpoint: "https://" + domain + "/oauth/v2/device_authorization",
				TokenEndpoint:               "https://" + domain + "/oauth/v2/token",
				ClientID:                    cmd.String("auth-device-client-id"),
			},
		})

		slog.Info("starting server", "addr", addr, "db", dbPath)
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	},
}
