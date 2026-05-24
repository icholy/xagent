package agent

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// reloadTimeout matches the runner's SIGTERM→SIGKILL window. If the
// agent subprocess doesn't exit within this window of receiving
// SIGHUP, the driver bails out non-zero so the runner's docker
// monitor emits `failed` as a fallback.
const reloadTimeout = 30 * time.Second

// Outcome classifies what happened during a single agent.Prompt call.
type Outcome int

const (
	// OutcomeCompleted indicates the agent finished its prompt
	// naturally. Late-arriving signals are ignored.
	OutcomeCompleted Outcome = iota
	// OutcomeStopped indicates SIGTERM cancelled the agent (graceful
	// stop). Wins over OutcomeReload when both signals race.
	OutcomeStopped
	// OutcomeReload indicates SIGHUP requested an in-place reload.
	// The caller should emit `started` and call Run again with the
	// resume prompt.
	OutcomeReload
	// OutcomeFailed indicates Prompt returned a non-cancellation
	// error.
	OutcomeFailed
)

// runState owns the signal-driven cancellation and outcome
// classification used by Driver.Run. The lifecycle is:
//
//   - mainCtx is cancelled by SIGTERM with [ErrStop]; cancellation
//     propagates to every per-iteration agent context.
//   - Each call to [runState.Run] creates a child agent context that
//     SIGHUP cancels with [ErrReload]. SIGTERM always wins because
//     classify checks mainCtx's cause first.
//
// runState also enforces the reload-hang safety net: if the agent
// doesn't return within [reloadTimeout] of receiving SIGHUP, the
// process exits non-zero so the runner's docker monitor emits
// `failed`.
type runState struct {
	log     *slog.Logger
	mainCtx context.Context
	cancel  context.CancelCauseFunc
	sigCh   chan os.Signal
	// exit is overridable in tests so the reload-hang safety net can
	// be exercised without killing the test binary.
	exit func(code int)

	mu          sync.Mutex
	agentCancel context.CancelCauseFunc
	reloading   bool
	reloadTimer *time.Timer
}

// newRunState wires SIGTERM and SIGHUP handlers into a fresh
// cancellation hierarchy derived from parent.
func newRunState(parent context.Context, log *slog.Logger) *runState {
	ctx, cancel := context.WithCancelCause(parent)
	s := &runState{
		log:     log,
		mainCtx: ctx,
		cancel:  cancel,
		sigCh:   make(chan os.Signal, 2),
		exit:    os.Exit,
	}
	// PID 1 ignores default-action signals — explicit Notify is mandatory.
	signal.Notify(s.sigCh, syscall.SIGTERM, syscall.SIGHUP)
	go s.handleSignals()
	return s
}

// Close detaches the signal handlers and cancels the main context.
// Safe to call multiple times.
func (s *runState) Close() {
	signal.Stop(s.sigCh)
	s.cancel(nil)
}

// Context returns the main context. Setup and other non-agent work
// should observe it so it cancels on SIGTERM.
func (s *runState) Context() context.Context {
	return s.mainCtx
}

// Run drives a single agent.Prompt call and classifies the outcome.
// The returned error is non-nil only for OutcomeFailed.
func (s *runState) Run(a Agent, prompt string, resume bool) (Outcome, error) {
	agentCtx, agentCancel := context.WithCancelCause(s.mainCtx)
	s.setAgentCancel(agentCancel)

	promptErr := a.Prompt(agentCtx, prompt, resume)

	s.setAgentCancel(nil)
	agentCancel(nil)

	return s.classify(promptErr, agentCtx)
}

func (s *runState) setAgentCancel(c context.CancelCauseFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentCancel = c
}

// classify maps a Prompt return into an Outcome. SIGTERM (mainCtx
// cause = ErrStop) wins over SIGHUP. A nil promptErr is always
// OutcomeCompleted, even if a signal raced in late — the caller
// already has the agent's result and shouldn't undo it.
func (s *runState) classify(promptErr error, agentCtx context.Context) (Outcome, error) {
	if promptErr == nil {
		return OutcomeCompleted, nil
	}
	if context.Cause(s.mainCtx) == ErrStop {
		return OutcomeStopped, nil
	}
	if context.Cause(agentCtx) == ErrReload {
		s.finishReload()
		return OutcomeReload, nil
	}
	return OutcomeFailed, promptErr
}

func (s *runState) handleSignals() {
	for {
		select {
		case <-s.mainCtx.Done():
			return
		case sig := <-s.sigCh:
			switch sig {
			case syscall.SIGTERM:
				s.stop()
			case syscall.SIGHUP:
				s.reload()
			}
		}
	}
}

// stop is called by the SIGTERM handler. Exposed for tests.
func (s *runState) stop() {
	s.log.Info("received SIGTERM, stopping agent")
	s.cancel(ErrStop)
}

// reload is called by the SIGHUP handler. Exposed for tests. Ignored
// if a reload is already in progress or no agent is currently
// running.
func (s *runState) reload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.reloading:
		s.log.Info("ignoring SIGHUP: reload already in progress")
	case s.agentCancel == nil:
		s.log.Info("ignoring SIGHUP: no active agent")
	default:
		s.log.Info("received SIGHUP, reloading agent")
		s.reloading = true
		s.agentCancel(ErrReload)
		s.reloadTimer = time.AfterFunc(reloadTimeout, func() {
			s.log.Error("reload timeout exceeded, exiting non-zero")
			s.exit(1)
		})
	}
}

func (s *runState) finishReload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reloadTimer != nil {
		s.reloadTimer.Stop()
		s.reloadTimer = nil
	}
	s.reloading = false
}
