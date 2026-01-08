package agent

import (
	"context"
	"fmt"
	"log/slog"
)

// Agent type constants.
const (
	TypeClaude = "claude"
	TypeDummy  = "dummy"
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
	agentType := opts.Type
	if agentType == "" {
		agentType = TypeClaude
	}

	switch agentType {
	case TypeClaude:
		return NewClaudeAgent(ClaudeAgentOptions{
			Cwd:        opts.Cwd,
			Log:        opts.Log,
			McpServers: opts.McpServers,
		}), nil
	case TypeDummy:
		return NewDummyAgent(opts.Log), nil
	default:
		return nil, fmt.Errorf("unknown agent type: %s", agentType)
	}
}
