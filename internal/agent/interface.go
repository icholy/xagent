package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// ErrStop is a sentinel error used to signal graceful agent cancellation.
// Callers can cancel the context with ErrStop as the cause:
//
//	ctx, cancel := context.WithCancelCause(parentCtx)
//	cancel(agent.ErrStop)
//
// After Prompt returns, callers can check context.Cause(ctx) == ErrStop
// to distinguish a graceful stop from other errors.
var ErrStop = errors.New("stop")

// Agent type constants.
const (
	TypeClaude  = "claude"
	TypeCopilot = "copilot"
	TypeCursor  = "cursor"
	TypeDummy   = "dummy"
)

// Agent abstracts the underlying agent implementation (e.g., Claude Code, Cursor).
type Agent interface {
	// Prompt sends a prompt to the agent and waits for completion.
	// If resume is true, the agent should continue from the previous session.
	//
	// If the provided context is cancelled, the returned error must have
	// context.Canceled in its error chain (i.e., errors.Is(err, context.Canceled)
	// must return true).
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
	Claude     *ClaudeOptions
	Copilot    *CopilotOptions
	Cursor     *CursorOptions
}

// ClaudeOptions contains Claude-specific agent options.
type ClaudeOptions struct {
	Model string
}

// CopilotOptions contains Copilot-specific agent options.
type CopilotOptions struct {
	Model string
}

// CursorOptions contains Cursor-specific agent options.
type CursorOptions struct {
	Model string
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
			options:    opts.Claude,
		}, nil
	case TypeCopilot:
		return &CopilotAgent{
			log:        log,
			cwd:        cmp.Or(opts.Cwd, "."),
			mcpServers: opts.McpServers,
			options:    opts.Copilot,
		}, nil
	case TypeCursor:
		return &CursorAgent{
			log:        log,
			cwd:        cmp.Or(opts.Cwd, "."),
			mcpServers: opts.McpServers,
			options:    opts.Cursor,
		}, nil
	case TypeDummy:
		return &DummyAgent{log: log}, nil
	default:
		return nil, fmt.Errorf("unknown agent type: %s", opts.Type)
	}
}
