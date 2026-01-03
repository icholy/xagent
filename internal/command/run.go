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

		// Start agent
		a, err := agent.Start(ctx, agent.Options{
			Cwd:        cwd,
			SessionID:  cfg.SessionID,
			McpServers: cfg.McpServers,
		})
		if err != nil {
			return err
		}
		defer a.Close()

		// Run the prompt
		if err := a.Prompt(ctx, prompt); err != nil {
			return err
		}

		// Ask agent to report links and problems
		if err := a.Prompt(ctx, strings.Join([]string{
			"IMPORTANT: Your text responses are NOT visible to end users.",
			"You MUST use the xagent MCP server tools to report information:",
			"",
			"1. Use xagent:create_link for any URLs related to this task (PRs, Jira tickets, GitHub issues, docs).",
			"   Set created=true if you created the resource, created=false if it already existed.",
			"",
			"2. Use xagent:report for any problems, blockers, assumptions, or important observations.",
			"",
			"Only information submitted via these tools will be visible in the task dashboard.",
			"Do not write a summary - use the tools. If you have nothing to report, do nothing.",
		}, "\n")); err != nil {
			slog.Error("report prompt failed", "error", err)
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
