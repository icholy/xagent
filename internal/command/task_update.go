package command

import (
	"context"
	"fmt"
	"strconv"

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
			Value:   "http://localhost:6464",
		},
		&cli.StringFlag{
			Name:    "name",
			Aliases: []string{"n"},
			Usage:   "Set task name",
		},
		&cli.BoolFlag{
			Name:    "start",
			Aliases: []string{"r"},
			Usage:   "Start the task (non-interrupting if already running)",
		},
		&cli.StringSliceFlag{
			Name:    "add-instruction",
			Aliases: []string{"i"},
			Usage:   "Add instruction to task (can be specified multiple times)",
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

		name := cmd.String("name")
		start := cmd.Bool("start")
		texts := cmd.StringSlice("add-instruction")

		if name == "" && !start && len(texts) == 0 {
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
			Start:           start,
			AddInstructions: instructions,
		}); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		fmt.Println("Task updated.")
		return nil
	},
}
