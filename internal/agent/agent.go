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

// Agent manages agent interactions via a Driver.
type Agent struct {
	driver Driver
}

// Start creates and starts a new Agent with a ClaudeDriver.
func Start(ctx context.Context, opts Options) (*Agent, error) {
	driver := NewClaudeDriver(ClaudeOptions{
		Cwd:        cmp.Or(opts.Cwd, "."),
		Log:        cmp.Or(opts.Log, slog.Default()),
		McpServers: opts.McpServers,
	})
	return &Agent{driver: driver}, nil
}

// Close shuts down the agent.
func (a *Agent) Close() error {
	return a.driver.Close()
}

// Prompt sends a prompt to the agent and waits for completion.
func (a *Agent) Prompt(ctx context.Context, prompt string, resume bool) error {
	return a.driver.Prompt(ctx, prompt, resume)
}
