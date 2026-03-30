package command

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/urfave/cli/v3"
)

var ShellCommand = &cli.Command{
	Name:      "shell",
	Usage:     "Open an interactive shell in a task container",
	ArgsUsage: "<task-id>",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "commit",
			Usage: "For stopped containers, commit the filesystem to a temporary image and run a shell in it",
		},
	},
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

		if c.State == "running" {
			dockerCmd := exec.CommandContext(ctx, "docker", "exec", "-it", c.ID[:12], "/bin/sh")
			dockerCmd.Stdin = os.Stdin
			dockerCmd.Stdout = os.Stdout
			dockerCmd.Stderr = os.Stderr
			return dockerCmd.Run()
		}

		if !cmd.Bool("commit") {
			return cli.Exit("container is not running (use --commit to shell into a stopped container)", 1)
		}

		// Commit the stopped container to a temporary image to preserve
		// the filesystem (including setup command artifacts like cloned repos).
		tmpImage := "xagent-shell-" + taskID
		resp, err := docker.ContainerCommit(ctx, c.ID, container.CommitOptions{
			Reference: tmpImage,
		})
		if err != nil {
			return fmt.Errorf("failed to commit container: %w", err)
		}

		// Clean up the temporary image when done.
		defer func() {
			if _, err := docker.ImageRemove(ctx, resp.ID, image.RemoveOptions{}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to remove temporary image: %v\n", err)
			}
		}()

		inspect, err := docker.ContainerInspect(ctx, c.ID)
		if err != nil {
			return fmt.Errorf("failed to inspect container: %w", err)
		}

		args := []string{"run", "-it", "--rm"}
		for _, b := range inspect.HostConfig.Binds {
			args = append(args, "-v", b)
		}
		if inspect.Config.WorkingDir != "" {
			args = append(args, "-w", inspect.Config.WorkingDir)
		}
		args = append(args, tmpImage, "/bin/sh")

		dockerCmd := exec.CommandContext(ctx, "docker", args...)
		dockerCmd.Stdin = os.Stdin
		dockerCmd.Stdout = os.Stdout
		dockerCmd.Stderr = os.Stderr
		return dockerCmd.Run()
	},
}
