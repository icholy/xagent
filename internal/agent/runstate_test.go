package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"gotest.tools/v3/assert"
)

// stubAgent runs promptFn for each Prompt call.
type stubAgent struct {
	promptFn func(ctx context.Context, prompt string, resume bool) error
}

func (s *stubAgent) Prompt(ctx context.Context, prompt string, resume bool) error {
	return s.promptFn(ctx, prompt, resume)
}

func (s *stubAgent) Close() error { return nil }

func newTestRunState(t *testing.T) *runState {
	t.Helper()
	s := newRunState(t.Context(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(s.Close)
	return s
}

func TestRunStateCompleted(t *testing.T) {
	s := newTestRunState(t)
	agent := &stubAgent{
		promptFn: func(ctx context.Context, prompt string, resume bool) error {
			return nil
		},
	}
	outcome, err := s.Run(agent, "hello", false)
	assert.NilError(t, err)
	assert.Equal(t, outcome, OutcomeCompleted)
}

func TestRunStateFailed(t *testing.T) {
	s := newTestRunState(t)
	boom := errors.New("boom")
	agent := &stubAgent{
		promptFn: func(ctx context.Context, prompt string, resume bool) error {
			return boom
		},
	}
	outcome, err := s.Run(agent, "hello", false)
	assert.Equal(t, outcome, OutcomeFailed)
	assert.ErrorIs(t, err, boom)
}

func TestRunStateStopped(t *testing.T) {
	s := newTestRunState(t)
	agent := &stubAgent{
		promptFn: func(ctx context.Context, prompt string, resume bool) error {
			s.stop()
			<-ctx.Done()
			return ctx.Err()
		},
	}
	outcome, err := s.Run(agent, "hello", false)
	assert.NilError(t, err)
	assert.Equal(t, outcome, OutcomeStopped)
}

func TestRunStateReload(t *testing.T) {
	s := newTestRunState(t)
	agent := &stubAgent{
		promptFn: func(ctx context.Context, prompt string, resume bool) error {
			s.reload()
			<-ctx.Done()
			return ctx.Err()
		},
	}
	outcome, err := s.Run(agent, "hello", false)
	assert.NilError(t, err)
	assert.Equal(t, outcome, OutcomeReload)
}

// SIGTERM wins over SIGHUP — even if both signals fire mid-Prompt,
// the outcome is OutcomeStopped.
func TestRunStateStopWinsOverReload(t *testing.T) {
	s := newTestRunState(t)
	agent := &stubAgent{
		promptFn: func(ctx context.Context, prompt string, resume bool) error {
			s.reload()
			s.stop()
			<-ctx.Done()
			return ctx.Err()
		},
	}
	outcome, err := s.Run(agent, "hello", false)
	assert.NilError(t, err)
	assert.Equal(t, outcome, OutcomeStopped)
}

// A naturally-completed Prompt is always OutcomeCompleted, even if
// SIGHUP arrives at the same moment — the driver doesn't go back to
// handle it.
func TestRunStateLateReloadIgnored(t *testing.T) {
	s := newTestRunState(t)
	agent := &stubAgent{
		promptFn: func(ctx context.Context, prompt string, resume bool) error {
			// Trigger reload but return nil to simulate the race
			// where SIGHUP arrives at the same moment the agent
			// returns success.
			s.reload()
			return nil
		},
	}
	outcome, err := s.Run(agent, "hello", false)
	assert.NilError(t, err)
	assert.Equal(t, outcome, OutcomeCompleted)
}

// SIGHUP while no agent is running should be a no-op.
func TestRunStateReloadIgnoredOutsideRun(t *testing.T) {
	s := newTestRunState(t)
	s.reload()
	agent := &stubAgent{
		promptFn: func(ctx context.Context, prompt string, resume bool) error {
			return nil
		},
	}
	outcome, err := s.Run(agent, "hello", false)
	assert.NilError(t, err)
	assert.Equal(t, outcome, OutcomeCompleted)
}

// After a reload, a subsequent Run should work normally and not stay
// in the "reloading" state.
func TestRunStateReloadClearsState(t *testing.T) {
	s := newTestRunState(t)
	first := &stubAgent{
		promptFn: func(ctx context.Context, prompt string, resume bool) error {
			s.reload()
			<-ctx.Done()
			return ctx.Err()
		},
	}
	outcome, err := s.Run(first, "hello", false)
	assert.NilError(t, err)
	assert.Equal(t, outcome, OutcomeReload)

	second := &stubAgent{
		promptFn: func(ctx context.Context, prompt string, resume bool) error {
			return nil
		},
	}
	outcome, err = s.Run(second, "hello", true)
	assert.NilError(t, err)
	assert.Equal(t, outcome, OutcomeCompleted)
}

// A second SIGHUP arriving during a reload should be ignored.
func TestRunStateConcurrentReloadIgnored(t *testing.T) {
	s := newTestRunState(t)
	var calls atomic.Int32
	agent := &stubAgent{
		promptFn: func(ctx context.Context, prompt string, resume bool) error {
			calls.Add(1)
			s.reload()
			// Second reload while the first is still in flight —
			// should be silently dropped, not cause anything weird.
			s.reload()
			<-ctx.Done()
			return ctx.Err()
		},
	}
	outcome, err := s.Run(agent, "hello", false)
	assert.NilError(t, err)
	assert.Equal(t, outcome, OutcomeReload)
	assert.Equal(t, calls.Load(), int32(1))
}
