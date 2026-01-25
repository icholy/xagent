package command

import (
	"context"
	"crypto/rand"
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
			Usage:   "Database file path",
			Value:   "data/xagent.db",
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
			Name:    "auth-redirect-uri",
			Usage:   "OAuth redirect URI after login",
			Sources: cli.EnvVars("XAGENT_AUTH_REDIRECT_URI"),
		},
		&cli.StringFlag{
			Name:    "auth-post-logout-uri",
			Usage:   "URI to redirect to after logout",
			Sources: cli.EnvVars("XAGENT_AUTH_POST_LOGOUT_URI"),
		},
		&cli.StringFlag{
			Name:    "auth-device-client-id",
			Usage:   "ZITADEL client ID for device flow (native app)",
			Sources: cli.EnvVars("XAGENT_AUTH_DEVICE_CLIENT_ID"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		addr := cmd.String("addr")
		dbPath := cmd.String("db")
		notifyFlag := cmd.Bool("notify")

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

		domain := cmd.String("auth-domain")
		auth, err := apiauth.New(ctx, apiauth.Config{
			Domain:        domain,
			ClientID:      cmd.String("auth-client-id"),
			ClientSecret:  cmd.String("auth-client-secret"),
			RedirectURI:   cmd.String("auth-redirect-uri"),
			PostLogoutURI: cmd.String("auth-post-logout-uri"),
			EncryptionKey: keygen(),
		})
		if err != nil {
			return fmt.Errorf("failed to initialize auth: %w", err)
		}

		srv := server.New(server.Options{
			Tasks:      tasks,
			Logs:       logs,
			Links:      links,
			Events:     events,
			Workspaces: workspaces,
			Notify:     notifyFlag,
			Auth:       auth,
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

// TODO: allow persistent encryption key for sessions across restarts
func keygen() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("failed to generate key: %v", err))
	}
	return key
}
