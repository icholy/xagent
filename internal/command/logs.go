package command

import (
	"context"
	"fmt"
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var LogsCommand = &cli.Command{
	Name:      "logs",
	Usage:     "Display logs for a task",
	ArgsUsage: "<task-id>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   "http://localhost:8080",
		},
		&cli.BoolFlag{
			Name:    "follow",
			Aliases: []string{"f"},
			Usage:   "Follow log output",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.NArg() < 1 {
			return fmt.Errorf("task ID is required")
		}
		taskID, err := parseTaskID(cmd.Args().First())
		if err != nil {
			return err
		}

		client := xagentclient.New(cmd.String("server"))
		follow := cmd.Bool("follow")

		var lastCount int
		for {
			resp, err := client.ListLogs(ctx, &xagentv1.ListLogsRequest{
				TaskId: taskID,
			})
			if err != nil {
				return fmt.Errorf("failed to list logs: %w", err)
			}

			// Print new entries
			for i := lastCount; i < len(resp.Entries); i++ {
				entry := resp.Entries[i]
				fmt.Printf("[%s] %s\n", entry.Type, entry.Content)
			}
			lastCount = len(resp.Entries)

			if !follow {
				break
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}

		return nil
	},
}

func parseTaskID(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(s, "%d", &id)
	if err != nil {
		return 0, fmt.Errorf("invalid task ID: %s", s)
	}
	return id, nil
}
