package agent

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"text/template"

	"github.com/icholy/xagent/internal/auth/agentauth"
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
	prompt, err := cfg.prompt()
	if err != nil {
		return fmt.Errorf("failed to build prompt: %w", err)
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

//go:embed PROMPT.md
var promptText string

var promptTemplate = template.Must(template.New("prompt").Parse(promptText))

// prompt builds the bootstrap prompt sent to the agent.
func (c *Config) prompt() (string, error) {
	var b strings.Builder
	err := promptTemplate.Execute(&b, struct {
		Started            bool
		HasChildTasksScope bool
		Prompt             string
	}{
		Started:            c.Started,
		HasChildTasksScope: c.hasScope(agentauth.ScopeChildTasks),
		Prompt:             c.Prompt,
	})
	if err != nil {
		return "", err
	}
	return b.String(), nil
}
