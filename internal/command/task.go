package command

import "github.com/urfave/cli/v3"

var TaskCommand = &cli.Command{
	Name:  "task",
	Usage: "Task management commands",
	Commands: []*cli.Command{
		TaskListCommand,
		TaskCreateCommand,
		TaskUpdateCommand,
		TaskDeleteCommand,
	},
}
