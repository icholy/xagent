package command

import (
	"context"
	"fmt"

	"github.com/icholy/xagent/internal/deviceauth"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

var LoginCommand = &cli.Command{
	Name:  "login",
	Usage: "Authenticate with the xagent server",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "Server URL",
			Value:   xagentclient.DefaultURL,
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
		&cli.StringFlag{
			Name:    "token-file",
			Usage:   "Path to store authentication tokens",
			Value:   "data/token.json",
			Sources: cli.EnvVars("XAGENT_TOKEN_FILE"),
		},
		&cli.StringFlag{
			Name:  "key-name",
			Usage: "Name for the API key",
			Value: "cli",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		serverAddr := cmd.String("server")
		if err := deviceauth.DeviceFlow(ctx, deviceauth.DeviceFlowOptions{
			DiscoveryURL: deviceauth.DiscoveryURL(serverAddr),
			ServerURL:    serverAddr,
			TokenFile:    cmd.String("token-file"),
			KeyName:      cmd.String("key-name"),
			Display: func(resp *oidc.DeviceAuthorizationResponse) error {
				fmt.Printf("\nTo authenticate, visit: %s\n\n", resp.VerificationURIComplete)
				fmt.Println("Waiting for authentication...")
				return nil
			},
		}); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		fmt.Println("Authentication successful!")
		return nil
	},
}
