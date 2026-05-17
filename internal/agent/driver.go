package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
)

type Driver struct {
	TaskID int64
	Client xagentclient.Client
	Log    *slog.Logger
}

func (d *Driver) Run(ctx context.Context) error {

	// Set up SIGTERM handler to cancel with ErrStop
	ctx, cancel := context.WithCancelCause(ctx)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	go func() {
		<-sigCh
		d.Log.Info("received SIGTERM, stopping agent")
		cancel(ErrStop)
	}()
	defer signal.Stop(sigCh)

	// Make sure the server is reachable
	if _, err := d.Client.Ping(ctx, &xagentv1.PingRequest{}); err != nil {
		return fmt.Errorf("failed to ping server: %w", err)
	}

	// Load config
	cfg, err := LoadConfig(d.TaskID)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	d.Log.Info("loaded config",
		"cwd", cfg.Cwd,
		"commands", cfg.Commands,
		"mcp_servers", len(cfg.McpServers),
		"setup", cfg.Setup,
		"started", cfg.Started,
	)

	// Run setup commands if not already done
	if !cfg.Setup {
		for _, command := range cfg.Commands {
			d.Log.Info("Running setup command", "command", command)
			c := exec.CommandContext(ctx, "sh", "-c", command)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("setup command failed: %w", err)
			}
		}
		cfg.Setup = true
		if err := SaveConfig(d.TaskID, cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	}

	// Start agent
	a, err := NewAgent(Options{
		Type:       cfg.Type,
		Cwd:        os.ExpandEnv(cfg.Cwd),
		Verbose:    cfg.Verbose,
		McpServers: cfg.McpServers,
		Claude:     cfg.Claude,
		Codex:      cfg.Codex,
		Copilot:    cfg.Copilot,
		Cursor:     cfg.Cursor,
		Sloppy:     cfg.Sloppy,
		Dummy:      cfg.Dummy,
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
			"Events are routed to tasks that have a link with subscribe=true matching the event URL.",
			"When creating links with xagent:create_link, ALWAYS set subscribe=true for resources you create (PRs, issues, comments), even if the task is complete. Others may respond and you'll need to handle those responses. Only use subscribe=false for reference links to external resources you didn't create.",
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
		if context.Cause(ctx) == ErrStop {
			d.Log.Info("agent stopped gracefully")
			return nil
		}
		return err
	}

	// Save config
	cfg.Started = true
	if err := SaveConfig(d.TaskID, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	d.Log.Info("Task completed successfully.")
	return nil
}
