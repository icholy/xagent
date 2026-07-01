package command

import (
	"context"

	"github.com/icholy/xagent/internal/gitcredential"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var GitCredentialCommand = &cli.Command{
	Name:   "git-credential",
	Usage:  "Git credential helper for GitHub App tokens",
	Hidden: true,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Usage:   "server URL",
			Value:   xagentclient.DefaultURL,
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
		&cli.StringFlag{
			Name:    "token",
			Usage:   "Authentication token",
			Sources: cli.EnvVars("XAGENT_TOKEN"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		client := xagentclient.New(xagentclient.Options{
			BaseURL: cmd.String("server"),
			Token:   cmd.String("token"),
		})
		return gitcredential.Run(ctx, cmd.Args().First(), cmd.Root().Reader, cmd.Root().Writer, client)
	},
}
