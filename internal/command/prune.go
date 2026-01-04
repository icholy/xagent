package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var PruneCommand = &cli.Command{
	Name:  "prune",
	Usage: "Delete containers for archived tasks",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   "http://localhost:8080",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		client := xagentclient.New(cmd.String("server"))

		// Get all archived tasks
		resp, err := client.ListTasks(ctx, &xagentv1.ListTasksRequest{
			Statuses: []string{"archived"},
		})
		if err != nil {
			return fmt.Errorf("failed to list archived tasks: %w", err)
		}

		if len(resp.Tasks) == 0 {
			fmt.Println("No archived tasks found.")
			return nil
		}

		// Build filter for docker rm command
		// We'll use the xagent.task label to filter containers
		var removed int
		for _, task := range resp.Tasks {
			taskIDStr := strconv.FormatInt(task.Id, 10)
			containerName := "xagent-" + taskIDStr

			// Try to remove the container
			c := exec.CommandContext(ctx, "docker", "rm", "-f", containerName)
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				// Container might not exist, which is fine
				// Only show error if it's not a "no such container" error
				if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
					fmt.Fprintf(os.Stderr, "Warning: failed to remove container %s: %v\n", containerName, err)
				}
			} else {
				removed++
				fmt.Printf("Removed container: %s (task %d)\n", containerName, task.Id)
			}
		}

		fmt.Printf("\nRemoved %d container(s) for %d archived task(s).\n", removed, len(resp.Tasks))
		return nil
	},
}
