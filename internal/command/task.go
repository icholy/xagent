package command

import (
	"fmt"

	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var TaskCommand = &cli.Command{
	Name:  "task",
	Usage: "Task management commands",
	Commands: []*cli.Command{
		TaskListCommand,
		TaskCreateCommand,
		TaskUpdateCommand,
		TaskDeleteCommand,
	},
}

var tokenFlag = &cli.StringFlag{
	Name:    "token",
	Usage:   "Authentication token (e.g. API key)",
	Sources: cli.EnvVars("XAGENT_TOKEN"),
}

func tokenSourceFromCmd(cmd *cli.Command) (xagentclient.TokenSource, error) {
	token := cmd.String("token")
	if token == "" {
		return nil, fmt.Errorf("token is required (set --token or XAGENT_TOKEN)")
	}
	return xagentclient.StaticTokenSource(token), nil
}
