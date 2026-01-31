package command

import (
	"context"
	"fmt"
	"strconv"

	"github.com/icholy/xagent/internal/deviceauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var TaskDeleteCommand = &cli.Command{
	Name:      "delete",
	Usage:     "Delete a task",
	ArgsUsage: "<task-id>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   xagentclient.DefaultURL,
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
		&cli.StringFlag{
			Name:    "token-file",
			Usage:   "Path to authentication token file",
			Value:   "data/token.json",
			Sources: cli.EnvVars("XAGENT_TOKEN_FILE"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		taskIDStr := cmd.Args().First()
		if taskIDStr == "" {
			return fmt.Errorf("task ID is required")
		}
		taskID, err := strconv.ParseInt(taskIDStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid task ID: %w", err)
		}

		serverURL := cmd.String("server")
		token, err := deviceauth.LoadToken(cmd.String("token-file"))
		if err != nil {
			return fmt.Errorf("failed to load token: %w", err)
		}
		if !token.Valid() {
			return fmt.Errorf("no valid token available, run login to authenticate")
		}
		client := xagentclient.New(xagentclient.Options{BaseURL: serverURL, Token: token.APIKey})
		if _, err := client.DeleteTask(ctx, &xagentv1.DeleteTaskRequest{Id: taskID}); err != nil {
			return fmt.Errorf("failed to delete task: %w", err)
		}

		fmt.Println("Task deleted.")
		return nil
	},
}
