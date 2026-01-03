package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
)

// Options contains configuration for creating an Agent.
type Options struct {
	Cwd        string
	Log        *slog.Logger
	Resume     bool
	McpServers map[string]McpServer
}

// Agent manages Claude CLI interactions.
type Agent struct {
	log        *slog.Logger
	cwd        string
	mcpServers map[string]McpServer
	resume     bool
}

// Start creates and starts a new Agent.
func Start(ctx context.Context, opts Options) (*Agent, error) {
	log := cmp.Or(opts.Log, slog.Default())
	return &Agent{
		log:        log,
		cwd:        cmp.Or(opts.Cwd, "."),
		mcpServers: opts.McpServers,
		resume:     opts.Resume,
	}, nil
}

// Close shuts down the agent.
func (a *Agent) Close() error {
	return nil
}

// Prompt sends a prompt to Claude and waits for completion.
func (a *Agent) Prompt(ctx context.Context, prompt string) error {
	a.log.Info("sending prompt", "text", prompt)

	args := []string{
		"@anthropic-ai/claude-code",
		"--dangerously-skip-permissions",
		"--strict-mcp-config",
	}

	// Add MCP config if present
	if len(a.mcpServers) > 0 {
		mcpConfig := map[string]any{"mcpServers": a.mcpServers}
		mcpJSON, err := json.Marshal(mcpConfig)
		if err != nil {
			return err
		}
		args = append(args, "--mcp-config", string(mcpJSON))
	}

	// Session handling
	if a.resume {
		args = append(args, "--continue")
	}
	a.resume = true

	args = append(args, "--print", prompt)

	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Dir = a.cwd
	cmd.Env = append(os.Environ(), "DISABLE_AUTOUPDATER=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
