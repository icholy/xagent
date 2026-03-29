package command

import (
	"context"
	"fmt"
	"strconv"

	"github.com/icholy/xagent/internal/configfile"
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
		cfg, err := configfile.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if cfg.Token == "" {
			return fmt.Errorf("not authenticated, run setup first")
		}
		client := xagentclient.New(xagentclient.Options{BaseURL: serverURL, Token: cfg.Token})
		if _, err := client.DeleteTask(ctx, &xagentv1.DeleteTaskRequest{Id: taskID}); err != nil {
			return fmt.Errorf("failed to delete task: %w", err)
		}

		fmt.Println("Task deleted.")
		return nil
	},
}
