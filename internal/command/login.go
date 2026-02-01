package command

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/icholy/xagent/internal/agentauth"
	"github.com/icholy/xagent/internal/deviceauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/tokenfile"
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

		// Save the API key to the token file
		token := &tokenfile.File{APIKey: resp.RawToken}
		if err := tokenfile.Save(token); err != nil {
			return fmt.Errorf("save token: %w", err)
		}

		// Generate the private key for agent authentication
		secretPath := filepath.Join(tokenfile.Dir(), "secret.key")
		if _, err := agentauth.LoadOrCreatePrivateKey(secretPath); err != nil {
			return fmt.Errorf("generate private key: %w", err)
		}

		fmt.Printf("Config written to %s\n", tokenfile.Dir())
		return nil
	},
}
