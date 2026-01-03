package main

import (
	"context"
	"fmt"
	"os"

	"github.com/icholy/xagent/internal/command"
	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name:  "xagent",
		Usage: "Async agent orchestrator for Claude Code",
		Commands: []*cli.Command{
			command.RunCommand,
			command.ServerCommand,
			command.RunnerCommand,
			command.TaskCommand,
			command.ContainersCommand,
			command.WorkspacesCommand,
			command.McpCommand,
			command.GithubCommand,
			command.JiraCommand,
			command.ShellCommand,
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
}
