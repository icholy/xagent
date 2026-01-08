package agent

import (
	"cmp"
	"context"
	"log/slog"
)

// Options contains configuration for creating an Agent.
type Options struct {
	Cwd        string
	Log        *slog.Logger
	McpServers map[string]McpServer
}

// Start creates and starts a new ClaudeAgent.
func Start(ctx context.Context, opts Options) (*ClaudeAgent, error) {
	agent := NewClaudeAgent(ClaudeAgentOptions{
		Cwd:        cmp.Or(opts.Cwd, "."),
		Log:        cmp.Or(opts.Log, slog.Default()),
		McpServers: opts.McpServers,
	})
	return agent, nil
}
