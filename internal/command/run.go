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
			"mcp_servers", len(cfg.McpServers),
			"setup", cfg.Setup,
			"started", cfg.Started,
		)

		// Run setup commands if not already done
		if !cfg.Setup {
			for _, command := range cfg.Commands {
				fmt.Printf("Running setup command: %s\n", command)
				c := exec.CommandContext(ctx, "sh", "-c", command)
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				if err := c.Run(); err != nil {
					return fmt.Errorf("setup command failed: %w", err)
				}
			}
			cfg.Setup = true
			if err := agent.SaveConfig(taskID, cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
		}

		// Start agent
		a, err := agent.NewAgent(agent.Options{
			Type:       cfg.Type,
			Cwd:        os.ExpandEnv(cfg.Cwd),
			McpServers: cfg.McpServers,
		})
		if err != nil {
			return fmt.Errorf("failed to create agent: %w", err)
		}
		defer a.Close()

		// Bootstrap prompt
		var prompt string
		if cfg.Started {
			prompt = "The task was updated. Check xagent:get_my_task and continue."
		} else {
			prompt = strings.Join([]string{
				"Use xagent:get_my_task to fetch your task instructions and execute them.",
				"If the task does not have a name, use xagent:update_my_task to set one.",
				"",
				"Each instruction has a 'text' field with the task and an optional 'url' field with the source URL.",
				"If you have questions, problems, or take no action, respond on the platform from the most recent instruction or event url.",
				"When responding on external platforms, always suffix your message with (task {id}) with your task id.",
				"",
				"The task may have linked events. Events provide additional context such as GitHub webhooks or external triggers.",
				"Events are routed to tasks that have a link with notify=true matching the event URL.",
				"When creating links with xagent:create_link, ALWAYS set notify=true for resources you create (PRs, issues, comments), even if the task is complete. Others may respond and you'll need to handle those responses. Only use notify=false for reference links to external resources you didn't create.",
				"Use xagent:update_child_task to delegate work to child tasks.",
				"",
				"When done, use xagent:create_link for any URLs you created (PRs, issues, etc).",
				"Always use web URLs that users can visit, not API URLs.",
				"Use xagent:report to log important observations.",
				"Only use xagent:create_child_task when explicitly instructed to create a new task.",
				"",
				"Your text responses are NOT visible to users - only tool calls matter.",
			}, "\n")
		}
		if cfg.Prompt != "" {
			prompt = prompt + "\n\n" + cfg.Prompt
		}

		if err := a.Prompt(ctx, prompt, cfg.Started); err != nil {
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
