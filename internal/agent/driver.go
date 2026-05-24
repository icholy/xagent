package agent

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/icholy/xagent/internal/auth/agentauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
)

// emitTimeout caps how long the driver waits for the reload-time
// `started` event to be acknowledged. It uses context.Background so
// the RPC outlives the agent context, which may already be cancelled.
const emitTimeout = 30 * time.Second

type Driver struct {
	TaskID int64
	Client xagentclient.Client
	Log    *slog.Logger
}

func (d *Driver) Run(ctx context.Context) error {
	state := newRunState(ctx, d.Log)
	defer state.Close()

	// Make sure the server is reachable.
	if _, err := d.Client.Ping(state.Context(), &xagentv1.PingRequest{}); err != nil {
		return fmt.Errorf("failed to ping server: %w", err)
	}

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

	if !cfg.Setup {
		for _, command := range cfg.Commands {
			d.Log.Info("Running setup command", "command", command)
			c := exec.CommandContext(state.Context(), "sh", "-c", command)
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

	for {
		prompt, err := cfg.prompt()
		if err != nil {
			return fmt.Errorf("failed to build prompt: %w", err)
		}

		outcome, promptErr := state.Run(a, prompt, cfg.Started)
		switch outcome {
		case OutcomeStopped:
			d.Log.Info("agent stopped gracefully")
			return nil

		case OutcomeReload:
			d.Log.Info("agent reloaded")
			// Future iterations must use the resume prompt.
			cfg.Started = true
			if err := SaveConfig(d.TaskID, cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			// No docker start/die event fires for an in-place reload,
			// so the runner's monitor cannot transition the task off
			// of running+restart. Emit `started` so the server moves
			// it back to running+none.
			if err := d.emitReloaded(); err != nil {
				return fmt.Errorf("failed to emit started after reload: %w", err)
			}

		case OutcomeFailed:
			return promptErr

		case OutcomeCompleted:
			cfg.Started = true
			if err := SaveConfig(d.TaskID, cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			d.Log.Info("Task completed successfully.")
			return nil
		}
	}
}

// emitReloaded notifies the server that the in-place SIGHUP reload
// finished. Uses Version: 0 (same convention as the runner's docker
// monitor).
func (d *Driver) emitReloaded() error {
	ctx, cancel := context.WithTimeout(context.Background(), emitTimeout)
	defer cancel()
	_, err := d.Client.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{
			TaskId: d.TaskID,
			Event:  string(model.RunnerEventStarted),
		}},
	})
	if err != nil {
		d.Log.Error("failed to submit started event after reload", "error", err)
		return err
	}
	d.Log.Info("submitted started event after reload")
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
