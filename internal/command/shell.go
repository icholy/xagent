package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/urfave/cli/v3"
)

var ShellCommand = &cli.Command{
	Name:      "shell",
	Usage:     "Open an interactive shell in a task container",
	ArgsUsage: "<task-id>",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.NArg() < 1 {
			return cli.Exit("task ID required", 1)
		}
		taskID := cmd.Args().First()

		docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return fmt.Errorf("failed to create docker client: %w", err)
		}
		defer docker.Close()

		containers, err := docker.ContainerList(ctx, container.ListOptions{
			All:     true,
			Filters: filters.NewArgs(filters.Arg("label", "xagent.task="+taskID)),
		})
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}
		if len(containers) == 0 {
			return cli.Exit("no container found for task: "+taskID, 1)
		}

		c := containers[0]

		// If the container is stopped, start it so we can exec into it
		// with its filesystem intact (including setup command artifacts
		// like cloned repos).
		stopOnExit := false
		if c.State != "running" {
			if err := docker.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
				return fmt.Errorf("failed to start container: %w", err)
			}
			stopOnExit = true
		}

		dockerCmd := exec.CommandContext(ctx, "docker", "exec", "-it", c.ID[:12], "/bin/sh")
		dockerCmd.Stdin = os.Stdin
		dockerCmd.Stdout = os.Stdout
		dockerCmd.Stderr = os.Stderr
		err = dockerCmd.Run()

		if stopOnExit {
			timeout := 0
			if stopErr := docker.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); stopErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to stop container: %v\n", stopErr)
			}
		}

		return err
	},
}
