package command

import (
	"cmp"
	"context"
	"fmt"
	"os"

	"github.com/icholy/xagent/internal/agentauth"
	"github.com/icholy/xagent/internal/configfile"
	"github.com/icholy/xagent/internal/deviceauth"
	"github.com/icholy/xagent/internal/workspace"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

func defaultKeyName() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "cli"
	}
	return hostname
}

var SetupCommand = &cli.Command{
	Name:  "setup",
	Usage: "Authenticate with the xagent server and generate keys",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "Server URL",
			Value:   xagentclient.DefaultURL,
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
		&cli.StringFlag{
			Name:  "key-name",
			Usage: "Name for the API key",
			Value: defaultKeyName(),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		// Load existing config (or empty if first run)
		cfg, err := configfile.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		// Generate private key if there isn't one
		if cfg.PrivateKey == nil {
			cfg.PrivateKey, err = agentauth.CreatePrivateKey()
			if err != nil {
				return fmt.Errorf("generate private key: %w", err)
			}
		}

		// Authenticate via device flow
		serverAddr := cmd.String("server")
		accessToken, err := deviceauth.DeviceFlow(ctx, deviceauth.DeviceFlowOptions{
			DiscoveryURL: deviceauth.DiscoveryURL(serverAddr),
			Display: func(resp *oidc.DeviceAuthorizationResponse) error {
				fmt.Printf("\nTo authenticate, visit: %s\n\n", resp.VerificationURIComplete)
				fmt.Println("Waiting for authentication...")
				return nil
			},
		})
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		// Use the short-lived OIDC token to create an API key
		client := xagentclient.New(xagentclient.Options{
			BaseURL:  serverAddr,
			Token:    accessToken,
			AuthType: "bearer",
		})
		resp, err := client.CreateKey(ctx, &xagentv1.CreateKeyRequest{
			Name: cmp.Or(cmd.String("key-name"), defaultKeyName()),
		})
		if err != nil {
			return fmt.Errorf("create API key: %w", err)
		}
		cfg.Token = resp.RawToken

		// Save config file
		if err := configfile.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		configPath, _ := configfile.Path()
		fmt.Printf("Config written to %s\n", configPath)

		// Create default workspaces.yaml if it doesn't exist
		if path, created, err := workspace.CreateDefault(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create default workspaces.yaml: %v\n", err)
		} else if created {
			fmt.Printf("Workspaces written to %s\n", path)
		}

		return nil
	},
}
