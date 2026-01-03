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
			Name:    "name",
			Aliases: []string{"n"},
			Usage:   "Set task name",
		},
		&cli.StringFlag{
			Name:  "status",
			Usage: "Set task status (pending, running, completed, failed)",
		},
		&cli.StringSliceFlag{
			Name:    "add-instruction",
			Aliases: []string{"i"},
			Usage:   "Add instruction to task (can be specified multiple times)",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		taskID := cmd.Args().First()
		if taskID == "" {
			return fmt.Errorf("task ID is required")
		}

		name := cmd.String("name")
		status := cmd.String("status")
		texts := cmd.StringSlice("add-instruction")

		if name == "" && status == "" && len(texts) == 0 {
			return fmt.Errorf("nothing to update")
		}

		instructions := make([]*xagentv1.Instruction, len(texts))
		for i, text := range texts {
			instructions[i] = &xagentv1.Instruction{Text: text}
		}

		client := xagentclient.New(cmd.String("server"))
		if _, err := client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
			Id:              taskID,
			Name:            name,
			Status:          status,
			AddInstructions: instructions,
		}); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		fmt.Println("Task updated.")
		return nil
	},
}
