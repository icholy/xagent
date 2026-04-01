package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/icholy/xagent/internal/configfile"
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
			Value:   xagentclient.DefaultURL,
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
		&cli.StringFlag{
			Name:    "name",
			Aliases: []string{"n"},
			Usage:   "Task name",
		},
		&cli.StringFlag{
			Name:     "runner",
			Aliases:  []string{"r"},
			Usage:    "Runner ID to assign this task to",
			Required: true,
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
		serverURL := cmd.String("server")
		cfg, err := configfile.Load(nil)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if cfg.Token == "" {
			return fmt.Errorf("not authenticated, run setup first")
		}
		client := xagentclient.New(xagentclient.Options{BaseURL: serverURL, Token: cfg.Token})

		texts := cmd.StringSlice("instruction")
		instructions := make([]*xagentv1.Instruction, len(texts))
		for i, text := range texts {
			instructions[i] = &xagentv1.Instruction{Text: text}
		}

		resp, err := client.CreateTask(ctx, &xagentv1.CreateTaskRequest{
			Name:         cmd.String("name"),
			Runner:       cmd.String("runner"),
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
