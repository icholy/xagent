package command

import (
	"context"
	"os"
	"os/exec"

	"github.com/urfave/cli/v3"
)

var ContainersCommand = &cli.Command{
	Name:  "containers",
	Usage: "List xagent containers",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "prune",
			Usage: "Remove all xagent containers",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		var c *exec.Cmd
		if cmd.Bool("prune") {
			c = exec.CommandContext(ctx, "docker", "container", "prune", "-f", "--filter=label=xagent=true")
		} else {
			c = exec.CommandContext(ctx, "docker", "ps", "-a", "--filter=label=xagent=true")
		}
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	},
}
