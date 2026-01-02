package command

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/icholy/xagent/internal/agent"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xacp"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var RunCommand = &cli.Command{
	Name:  "run",
	Usage: "Run an agent for a task",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "cwd",
			Aliases: []string{"C"},
			Usage:   "Working directory for the agent",
		},
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
		cwd := cmd.String("cwd")
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		taskID := cmd.String("task")
		client := xagentclient.New(cmd.String("server"))

		// Load config
		cfg, err := agent.LoadConfig(taskID)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		slog.Info("loaded config",
			"commands", cfg.Commands,
			"command_index", cfg.CommandIndex,
			"session_id", cfg.SessionID,
			"prompt_index", cfg.PromptIndex,
		)

		// Run pending setup commands
		for cfg.CommandIndex < len(cfg.Commands) {
			command := cfg.Commands[cfg.CommandIndex]
			fmt.Printf("Running setup command: %s\n", command)
			c := exec.CommandContext(ctx, "sh", "-c", command)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("setup command failed: %w", err)
			}
			cfg.CommandIndex++
			if err := agent.SaveConfig(taskID, cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
		}

		// Fetch task
		resp, err := client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
		if err != nil {
			return fmt.Errorf("failed to fetch task: %w", err)
		}
		task := resp.Task

		prompt := combinedPrompt(task, cfg.PromptIndex)
		if prompt == "" {
			fmt.Println("No prompts to execute.")
			return nil
		}

		// Setup logger
		logger := xacp.NewUpdateLogger(xacp.UpdateLoggerOptions{
			Log:       slog.Default(),
			BatchSize: 1,
		})

		// Start agent
		a, err := agent.Start(ctx, agent.Options{
			Cwd:        cwd,
			SessionID:  cfg.SessionID,
			McpServers: cfg.McpServers,
			ACP:        cfg.ACP,
			OnUpdate:   logger.Update,
		})
		if err != nil {
			return err
		}
		defer a.Close()

		// Run the prompt
		if err := a.Prompt(ctx, prompt); err != nil {
			return err
		}

		logger.Flush()

		// Ask agent to report any links it created
		if err := a.Prompt(ctx, strings.Join([]string{
			"If you created any pull requests, Jira tickets, or other external",
			"resources during this task, use the create_link tool to report each one.",
			"If you did not create any, do nothing.",
		}, " ")); err != nil {
			slog.Error("links prompt failed", "error", err)
		}

		// Save config
		cfg.SessionID = a.SessionID()
		cfg.PromptIndex = len(task.Prompts)
		if err := agent.SaveConfig(taskID, cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Println("Task completed successfully.")
		return nil
	},
}

func combinedPrompt(t *xagentv1.Task, offset int) string {
	prompts := t.GetPrompts()
	if offset >= len(prompts) {
		return ""
	}
	return strings.Join(prompts[offset:], "\n")
}
