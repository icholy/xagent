package command

import (
	"context"
	"fmt"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var TaskUpdateCommand = &cli.Command{
	Name:      "update",
	Usage:     "Update a task",
	ArgsUsage: "<task-id>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   "http://localhost:8080",
		},
		&cli.StringFlag{
			Name:  "status",
			Usage: "Set task status (pending, running, completed, failed)",
		},
		&cli.StringSliceFlag{
			Name:    "add-prompt",
			Aliases: []string{"p"},
			Usage:   "Add prompt to task (can be specified multiple times)",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		taskID := cmd.Args().First()
		if taskID == "" {
			return fmt.Errorf("task ID is required")
		}

		status := cmd.String("status")
		prompts := cmd.StringSlice("add-prompt")

		if status == "" && len(prompts) == 0 {
			return fmt.Errorf("nothing to update")
		}

		client := xagentclient.New(cmd.String("server"))
		if _, err := client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
			Id:         taskID,
			Status:     status,
			AddPrompts: prompts,
		}); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		fmt.Println("Task updated.")
		return nil
	},
}
