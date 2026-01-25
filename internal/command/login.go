package command

import (
	"context"
	"fmt"

	"github.com/icholy/xagent/internal/deviceauth"
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
			Value:   "http://localhost:6464",
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
		&cli.StringFlag{
			Name:    "token-file",
			Usage:   "Path to store authentication tokens",
			Value:   "data/token.json",
			Sources: cli.EnvVars("XAGENT_TOKEN_FILE"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		auth, err := deviceauth.New(ctx, deviceauth.Options{
			DiscoveryURL: deviceauth.DiscoveryURL(cmd.String("server")),
			TokenFile:    cmd.String("token-file"),
			Display: func(resp *oidc.DeviceAuthorizationResponse) error {
				fmt.Printf("\nTo authenticate, visit: %s\n\n", resp.VerificationURIComplete)
				fmt.Println("Waiting for authentication...")
				return nil
			},
		})
		if err != nil {
			return fmt.Errorf("failed to initialize auth: %w", err)
		}

		if err := auth.DeviceFlow(ctx); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		fmt.Println("Authentication successful!")
		return nil
	},
}
