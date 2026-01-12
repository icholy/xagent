package agent

import (
	"context"
	"log/slog"
)

// DummyAgent is a no-op agent implementation for testing.
type DummyAgent struct {
	log *slog.Logger
}

// Prompt does nothing and returns nil.
// If the context is cancelled with ErrStop as the cause, it returns ErrStop.
func (a *DummyAgent) Prompt(ctx context.Context, prompt string, resume bool) error {
	a.log.Info("dummy agent received prompt", "text", prompt, "resume", resume)
	if context.Cause(ctx) == ErrStop {
		return ErrStop
	}
	return nil
}

// Close does nothing and returns nil.
func (a *DummyAgent) Close() error {
	return nil
}
