package command

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/deviceauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/oauthflow"
	"github.com/icholy/xagent/internal/otelx"
	"github.com/icholy/xagent/internal/pubsub"
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
			Sources: cli.EnvVars("XAGENT_DATABASE_URL"),
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
		&cli.StringFlag{
			Name:    "auth-app-key",
			Usage:   "Hex-encoded 32-byte Ed25519 seed for signing app JWTs (generated if not set)",
			Sources: cli.EnvVars("XAGENT_AUTH_APP_KEY"),
		},
		&cli.BoolFlag{
			Name:  "no-auth",
			Usage: "Disable authentication (for development only)",
		},
		&cli.BoolFlag{
			Name:    "cors",
			Usage:   "Enable permissive CORS headers (for development only)",
			Sources: cli.EnvVars("XAGENT_CORS"),
		},
		&cli.StringFlag{
			Name:    "github-app-id",
			Usage:   "GitHub App ID",
			Sources: cli.EnvVars("XAGENT_GITHUB_APP_ID"),
		},
		&cli.StringFlag{
			Name:    "github-app-slug",
			Usage:   "GitHub App slug (for install URL)",
			Sources: cli.EnvVars("XAGENT_GITHUB_APP_SLUG"),
		},
		&cli.StringFlag{
			Name:    "github-client-id",
			Usage:   "GitHub App OAuth client ID",
			Sources: cli.EnvVars("XAGENT_GITHUB_CLIENT_ID"),
		},
		&cli.StringFlag{
			Name:    "github-client-secret",
			Usage:   "GitHub App OAuth client secret",
			Sources: cli.EnvVars("XAGENT_GITHUB_CLIENT_SECRET"),
		},
		&cli.StringFlag{
			Name:    "github-webhook-secret",
			Usage:   "GitHub App webhook secret",
			Sources: cli.EnvVars("XAGENT_GITHUB_WEBHOOK_SECRET"),
		},
		&cli.StringFlag{
			Name:    "atlassian-client-id",
			Usage:   "Atlassian OAuth client ID (for account linking)",
			Sources: cli.EnvVars("XAGENT_ATLASSIAN_CLIENT_ID"),
		},
		&cli.StringFlag{
			Name:    "atlassian-client-secret",
			Usage:   "Atlassian OAuth client secret",
			Sources: cli.EnvVars("XAGENT_ATLASSIAN_CLIENT_SECRET"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		addr := cmd.String("addr")
		dbPath := cmd.String("db")
		noAuth := cmd.Bool("no-auth")

		// Initialize OpenTelemetry (configured via OTEL_EXPORTER_OTLP_ENDPOINT env var)
		otel, err := otelx.Setup(ctx)
		if err != nil {
			return fmt.Errorf("failed to initialize OpenTelemetry: %w", err)
		}
		defer otel.Shutdown(ctx)

		db, err := store.Open(dbPath, true)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		st := store.New(db)

		domain := cmd.String("auth-domain")
		baseURL := cmd.String("base-url")
		key, err := apiauth.DecodeEncryptionKey(cmd.String("auth-encryption-key"))
		if err != nil && !noAuth {
			return fmt.Errorf("invalid encryption key: %w", err)
		}
		appKey, err := apiauth.DecodeAppKey(cmd.String("auth-app-key"))
		if err != nil {
			return fmt.Errorf("invalid app key: %w", err)
		}
		if appKey == nil {
			appKey, err = apiauth.CreateAppPrivateKey()
			if err != nil {
				return fmt.Errorf("failed to generate app key: %w", err)
			}
		}
		resolver := &storeUserResolver{store: st}
		var devUser *apiauth.UserInfo
		if noAuth {
			slog.Warn("SSO authentication disabled, using dev user")
			devUser = &apiauth.UserInfo{
				ID:    "dev",
				Email: "dev@localhost",
				Name:  "Developer",
			}
			if err := resolver.Provision(ctx, devUser); err != nil {
				return fmt.Errorf("failed to provision dev user: %w", err)
			}
		}
		auth, err := apiauth.New(ctx, apiauth.Config{
			Domain:        domain,
			ClientID:      cmd.String("auth-client-id"),
			ClientSecret:  cmd.String("auth-client-secret"),
			RedirectURI:   baseURL + "/auth/callback",
			PostLogoutURI: baseURL,
			EncryptionKey: key,
			KeyValidator:  &storeKeyValidator{store: st},
			UserResolver:  resolver,
			AppKey:        appKey,
			DevUser:       devUser,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize auth: %w", err)
		}

		oauth, err := oauthflow.New(oauthflow.Options{
			AppKey:  appKey,
			BaseURL: baseURL,
		})
		if err != nil {
			return fmt.Errorf("failed to initialize oauth: %w", err)
		}
		ps := pubsub.NewLocalPubSub()
		opts := server.Options{
			Store:         st,
			Auth:          auth,
			BaseURL:       baseURL,
			EncryptionKey: key,
			OAuth:         oauth,
			CORS:          cmd.Bool("cors"),
			Publisher:     ps,
			Subscriber:    ps,
			Discovery: deviceauth.DiscoveryConfig{
				DeviceAuthorizationEndpoint: "https://" + domain + "/oauth/v2/device_authorization",
				TokenEndpoint:               "https://" + domain + "/oauth/v2/token",
				ClientID:                    cmd.String("auth-device-client-id"),
			},
		}
		if ghClientID := cmd.String("github-client-id"); ghClientID != "" {
			opts.GitHub = &server.GitHubConfig{
				AppID:         cmd.String("github-app-id"),
				AppSlug:       cmd.String("github-app-slug"),
				ClientID:      ghClientID,
				ClientSecret:  cmd.String("github-client-secret"),
				WebhookSecret: cmd.String("github-webhook-secret"),
			}
		}
		if atlassianClientID := cmd.String("atlassian-client-id"); atlassianClientID != "" {
			opts.Atlassian = &server.AtlassianConfig{
				ClientID:     atlassianClientID,
				ClientSecret: cmd.String("atlassian-client-secret"),
			}
		}
		srv := server.New(opts)

		httpServer := &http.Server{
			Addr:    addr,
			Handler: srv.Handler(),
		}

		// Set up signal handler for graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			sig := <-sigCh
			slog.Info("received signal, shutting down", "signal", sig)

			// Give active requests time to complete
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				slog.Error("shutdown error", "error", err)
			}
		}()

		slog.Info("starting server", "addr", addr, "db", dbPath)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server error: %w", err)
		}

		slog.Info("server stopped")
		return nil
	},
}

// storeKeyValidator implements apiauth.KeyValidator using the store.
type storeKeyValidator struct {
	store *store.Store
}

func (v *storeKeyValidator) ValidateKey(ctx context.Context, keyHash string) (*apiauth.UserInfo, error) {
	key, err := v.store.GetKeyByHash(ctx, nil, keyHash)
	if err != nil {
		return nil, err
	}
	if key.IsExpired() {
		return nil, fmt.Errorf("key expired")
	}
	return &apiauth.UserInfo{OrgID: key.OrgID, Name: key.Name, Type: apiauth.AuthTypeKey}, nil
}

// storeUserResolver implements apiauth.UserResolver using the store.
type storeUserResolver struct {
	store *store.Store
}

func (r *storeUserResolver) Provision(ctx context.Context, user *apiauth.UserInfo) error {
	return r.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		u := &model.User{
			ID:    user.ID,
			Email: user.Email,
			Name:  user.Name,
		}
		if err := r.store.UpsertUser(ctx, tx, u); err != nil {
			return err
		}
		// If the user has no default org, create one
		if u.DefaultOrgID == 0 {
			org := &model.Org{
				Name:  user.Name + "'s Org",
				Owner: user.ID,
			}
			if err := r.store.CreateOrg(ctx, tx, org); err != nil {
				return err
			}
			if err := r.store.AddOrgMember(ctx, tx, &model.OrgMember{
				OrgID:  org.ID,
				UserID: user.ID,
				Role:   "owner",
			}); err != nil {
				return err
			}
			if err := r.store.UpdateDefaultOrgID(ctx, tx, user.ID, org.ID); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

func (r *storeUserResolver) ResolveOrg(ctx context.Context, userID string, orgID int64) (int64, error) {
	// Fall back to the user's default org if none requested
	if orgID == 0 {
		u, err := r.store.GetUser(ctx, nil, userID)
		if err != nil {
			return 0, err
		}
		if u.DefaultOrgID == 0 {
			return 0, fmt.Errorf("user %s has no default org", userID)
		}
		orgID = u.DefaultOrgID
	}
	// Validate membership
	ok, err := r.store.IsOrgMember(ctx, nil, orgID, userID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("user %s is not a member of org %d", userID, orgID)
	}
	return orgID, nil
}
