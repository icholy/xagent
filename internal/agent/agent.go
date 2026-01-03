package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"

	"github.com/google/uuid"
)

// Options contains configuration for creating an Agent.
type Options struct {
	Cwd        string
	Log        *slog.Logger
	SessionID  string
	McpServers map[string]McpServer
}

// Agent manages Claude CLI interactions.
type Agent struct {
	log        *slog.Logger
	cwd        string
	sessionID  string
	mcpServers map[string]McpServer
	prompted   bool
}

// Start creates and starts a new Agent.
func Start(ctx context.Context, opts Options) (*Agent, error) {
	log := cmp.Or(opts.Log, slog.Default())

	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
		log.Info("generated session id", "id", sessionID)
	}

	return &Agent{
		log:        log,
		cwd:        cmp.Or(opts.Cwd, "."),
		sessionID:  sessionID,
		mcpServers: opts.McpServers,
	}, nil
}

// SessionID returns the session ID.
func (a *Agent) SessionID() string {
	return a.sessionID
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
		"--print",
		"--dangerously-skip-permissions",
		"--strict-mcp-config",
	}

	// Add MCP config if present
	if len(a.mcpServers) > 0 {
		mcpJSON, err := json.Marshal(a.mcpServers)
		if err != nil {
			return err
		}
		args = append(args, "--mcp-config", string(mcpJSON))
	}

	// Session handling
	if a.prompted {
		args = append(args, "--resume", a.sessionID)
	} else {
		args = append(args, "--session-id", a.sessionID)
	}

	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Dir = a.cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	a.prompted = true
	return nil
}
