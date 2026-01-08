package agent

import (
	"context"
)

// Driver abstracts the underlying agent implementation (e.g., Claude Code, Cursor).
type Driver interface {
	// Prompt sends a prompt to the agent and waits for completion.
	// If resume is true, the agent should continue from the previous session.
	Prompt(ctx context.Context, prompt string, resume bool) error

	// Close releases any resources held by the driver.
	Close() error
}
