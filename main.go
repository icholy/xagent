package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/urfave/cli/v3"
	"github.com/icholy/xagent/internal/command"
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
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
