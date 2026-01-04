package command

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

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

		// List all xagent containers and extract task IDs
		c := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter=label=xagent=true", "--format={{.Label \"xagent.task\"}}\t{{.Names}}")
		var out bytes.Buffer
		c.Stdout = &out
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}

		// Parse container list
		var containersToRemove []struct {
			taskID int64
			name   string
		}

		scanner := bufio.NewScanner(&out)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) != 2 {
				continue
			}

			taskIDStr := parts[0]
			containerName := parts[1]

			taskID, err := strconv.ParseInt(taskIDStr, 10, 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: invalid task ID %s for container %s\n", taskIDStr, containerName)
				continue
			}

			// Check if task is archived
			task, err := client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get task %d: %v\n", taskID, err)
				continue
			}

			if task.Task.Status == "archived" {
				containersToRemove = append(containersToRemove, struct {
					taskID int64
					name   string
				}{taskID, containerName})
			}
		}

		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading container list: %w", err)
		}

		if len(containersToRemove) == 0 {
			fmt.Println("No containers for archived tasks found.")
			return nil
		}

		// Remove containers
		var removed int
		for _, container := range containersToRemove {
			c := exec.CommandContext(ctx, "docker", "rm", "-f", container.name)
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to remove container %s: %v\n", container.name, err)
			} else {
				removed++
				fmt.Printf("Removed container: %s (task %d)\n", container.name, container.taskID)
			}
		}

		fmt.Printf("\nRemoved %d container(s).\n", removed)
		return nil
	},
}
