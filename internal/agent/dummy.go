package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/icholy/xagent/internal/common"
)

// DummyAgent is a no-op agent implementation for testing.
type DummyAgent struct {
	log     *slog.Logger
	options *DummyOptions
}

// Prompt handles the prompt based on the configured options.
// If Sleep is set to -1, it sleeps forever (until context cancellation).
// If Sleep is set to a positive value, it sleeps for that many seconds.
// Otherwise, it does nothing and returns nil.
func (a *DummyAgent) Prompt(ctx context.Context, prompt string, resume bool) error {
	a.log.Info("dummy agent received prompt", "text", prompt, "resume", resume)

	if a.options != nil && a.options.Sleep != 0 {
		if a.options.Sleep == -1 {
			a.log.Info("dummy agent sleeping forever")
			<-ctx.Done()
			return ctx.Err()
		}
		duration := time.Duration(a.options.Sleep) * time.Second
		a.log.Info("dummy agent sleeping", "duration", duration)
		if !common.SleepContext(ctx, duration) {
			return ctx.Err()
		}
	}

	return nil
}

// Close does nothing and returns nil.
func (a *DummyAgent) Close() error {
	return nil
}
