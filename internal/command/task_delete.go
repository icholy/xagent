package command

import (
	"context"
	"fmt"
	"strconv"

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
			Value:   "http://localhost:6464",
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

		client := xagentclient.New(cmd.String("server"), nil)
		if _, err := client.DeleteTask(ctx, &xagentv1.DeleteTaskRequest{Id: taskID}); err != nil {
			return fmt.Errorf("failed to delete task: %w", err)
		}

		fmt.Println("Task deleted.")
		return nil
	},
}
