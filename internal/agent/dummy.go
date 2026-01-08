package agent

import (
	"context"
	"log/slog"
)

// DummyAgent is a no-op agent implementation for testing.
type DummyAgent struct {
	log *slog.Logger
}

// NewDummyAgent creates a new DummyAgent.
func NewDummyAgent(log *slog.Logger) *DummyAgent {
	if log == nil {
		log = slog.Default()
	}
	return &DummyAgent{log: log}
}

// Prompt does nothing and returns nil.
func (a *DummyAgent) Prompt(ctx context.Context, prompt string, resume bool) error {
	a.log.Info("dummy agent received prompt", "text", prompt, "resume", resume)
	return nil
}

// Close does nothing and returns nil.
func (a *DummyAgent) Close() error {
	return nil
}
