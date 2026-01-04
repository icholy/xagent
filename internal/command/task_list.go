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

var TaskListCommand = &cli.Command{
	Name:  "list",
	Usage: "List tasks from the server",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   "http://localhost:8080",
		},
		&cli.StringSliceFlag{
			Name:  "status",
			Usage: "Filter by status (pending, running, completed, failed)",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		client := xagentclient.New(cmd.String("server"))

		resp, err := client.ListTasks(ctx, &xagentv1.ListTasksRequest{
			Statuses: cmd.StringSlice("status"),
		})
		if err != nil {
			return fmt.Errorf("failed to list tasks: %w", err)
		}

		// Fetch detailed information for each task
		var taskDetails []*xagentv1.GetTaskDetailsResponse
		for _, task := range resp.Tasks {
			detailResp, err := client.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{
				Id: task.Id,
			})
			if err != nil {
				return fmt.Errorf("failed to get task details for task %d: %w", task.Id, err)
			}
			taskDetails = append(taskDetails, detailResp)
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(taskDetails)
	},
}
