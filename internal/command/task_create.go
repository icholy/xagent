package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var TaskCreateCommand = &cli.Command{
	Name:  "create",
	Usage: "Create a new task",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   "http://localhost:8080",
		},
		&cli.StringFlag{
			Name:  "id",
			Usage: "Task ID (optional, auto-generated if not provided)",
		},
		&cli.StringFlag{
			Name:    "workspace",
			Aliases: []string{"w"},
			Usage:   "Workspace to use",
			Value:   "default",
		},
		&cli.StringSliceFlag{
			Name:    "instruction",
			Aliases: []string{"i"},
			Usage:   "Instruction to execute (can be specified multiple times)",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		client := xagentclient.New(cmd.String("server"))

		texts := cmd.StringSlice("instruction")
		instructions := make([]*xagentv1.Instruction, len(texts))
		for i, text := range texts {
			instructions[i] = &xagentv1.Instruction{Text: text}
		}

		resp, err := client.CreateTask(ctx, &xagentv1.CreateTaskRequest{
			Id:           cmd.String("id"),
			Workspace:    cmd.String("workspace"),
			Instructions: instructions,
		})
		if err != nil {
			return fmt.Errorf("failed to create task: %w", err)
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp.Task)
	},
}
