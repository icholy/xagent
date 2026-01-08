package agent

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
)

// Agent type constants.
const (
	TypeClaude  = "claude"
	TypeCopilot = "copilot"
	TypeDummy   = "dummy"
)

// Agent abstracts the underlying agent implementation (e.g., Claude Code, Cursor).
type Agent interface {
	// Prompt sends a prompt to the agent and waits for completion.
	// If resume is true, the agent should continue from the previous session.
	Prompt(ctx context.Context, prompt string, resume bool) error

	// Close releases any resources held by the agent.
	Close() error
}

// Options contains configuration for creating an Agent.
type Options struct {
	Type       string
	Cwd        string
	Log        *slog.Logger
	McpServers map[string]McpServer
}

// NewAgent creates an Agent based on the type specified in options.
// If Type is empty, it defaults to TypeClaude.
func NewAgent(opts Options) (Agent, error) {
	log := cmp.Or(opts.Log, slog.Default())

	switch cmp.Or(opts.Type, TypeClaude) {
	case TypeClaude:
		return &ClaudeAgent{
			log:        log,
			cwd:        cmp.Or(opts.Cwd, "."),
			mcpServers: opts.McpServers,
		}, nil
	case TypeCopilot:
		return &CopilotAgent{
			log:        log,
			cwd:        cmp.Or(opts.Cwd, "."),
			mcpServers: opts.McpServers,
		}, nil
	case TypeDummy:
		return &DummyAgent{log: log}, nil
	default:
		return nil, fmt.Errorf("unknown agent type: %s", opts.Type)
	}
}
