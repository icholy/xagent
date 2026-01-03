package command

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/icholy/xagent/internal/agent"
	"github.com/urfave/cli/v3"
)

var RunCommand = &cli.Command{
	Name:  "run",
	Usage: "Run an agent for a task",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   "http://localhost:8080",
		},
		&cli.StringFlag{
			Name:     "task",
			Aliases:  []string{"t"},
			Usage:    "Task ID to execute",
			Required: true,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		taskID := cmd.String("task")

		// Load config
		cfg, err := agent.LoadConfig(taskID)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		slog.Info("loaded config",
			"cwd", cfg.Cwd,
			"commands", cfg.Commands,
			"started", cfg.Started,
		)

		// Run setup commands if not started yet
		if !cfg.Started {
			for _, command := range cfg.Commands {
				fmt.Printf("Running setup command: %s\n", command)
				c := exec.CommandContext(ctx, "sh", "-c", command)
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				if err := c.Run(); err != nil {
					return fmt.Errorf("setup command failed: %w", err)
				}
			}
		}

		// Start agent
		a, err := agent.Start(ctx, agent.Options{
			Cwd:        os.ExpandEnv(cfg.Cwd),
			Resume:     cfg.Started,
			McpServers: cfg.McpServers,
		})
		if err != nil {
			return err
		}
		defer a.Close()

		// Bootstrap prompt
		var prompt string
		if cfg.Started {
			prompt = "The task was updated. Check xagent:get_task and continue."
		} else {
			prompt = strings.Join([]string{
				"Use xagent:get_task to fetch your task prompts and execute them.",
				"",
				"Prompts often include a URL indicating where they came from (Jira issue, GitHub PR, etc).",
				"If you have questions or problems, respond on that platform using the appropriate MCP tools.",
				"",
				"When done, use xagent:create_link for any URLs you created (PRs, issues, etc).",
				"Use xagent:report to log important observations.",
				"",
				"Your text responses are NOT visible to users - only tool calls matter.",
			}, "\n")
		}

		if err := a.Prompt(ctx, prompt); err != nil {
			return err
		}

		// Save config
		cfg.Started = true
		if err := agent.SaveConfig(taskID, cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Println("Task completed successfully.")
		return nil
	},
}
