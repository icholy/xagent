package agent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/icholy/xagent/internal/auth/agentauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
)

// StopTimeout is how long a cancelled agent run is given to unwind before
// escalating: the runner SIGKILLs a container this long after SIGTERM, and
// the driver exits non-zero if the agent doesn't return this long after a
// cancellation.
const StopTimeout = 30 * time.Second

// errUnwindTimeout means a cancelled agent run did not return within
// StopTimeout. The driver exits non-zero without reporting an event so the
// monitor's failed fallback takes over when the container dies.
var errUnwindTimeout = errors.New("agent did not exit after cancellation")

type Driver struct {
	TaskID int64
	Client xagentclient.Client
	Log    *slog.Logger
}

// Run executes the task's agent and reports the outcome to the server as
// runner events. The driver is the source of truth for started / stopped /
// failed; the runner's docker monitor only covers processes that die before
// they can report. The invariant: Run returns non-zero only when it could not
// report its outcome itself — an acked stopped or failed event means exit 0.
func (d *Driver) Run(ctx context.Context) error {
	// emitCtx survives signal-driven cancellation so terminal events can still
	// be submitted and acked after the run context is torn down. Each submit
	// is bounded by the client's HTTP timeout.
	emitCtx := context.WithoutCancel(ctx)

	// stopCtx is cancelled only by SIGTERM (ErrStop). It is the parent of
	// every run context, so a stop always cancels the current run, and a stop
	// that races with an in-progress reload wins.
	stopCtx, stopCancel := context.WithCancelCause(ctx)
	defer stopCancel(nil)

	// Each agent run gets its own context layered on stopCtx so SIGHUP can
	// cancel just the current run with ErrReload.
	var mu sync.Mutex
	var runCancel context.CancelCauseFunc
	newRun := func() context.Context {
		mu.Lock()
		defer mu.Unlock()
		runCtx, cancel := context.WithCancelCause(stopCtx)
		runCancel = cancel
		return runCtx
	}
	runCtx := newRun()

	// The driver is PID 1 in the container: the kernel ignores default-action
	// signals delivered to PID 1, so both signals must be explicitly handled.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case sig := <-sigCh:
				switch sig {
				case syscall.SIGTERM:
					d.Log.Info("received SIGTERM, stopping agent")
					stopCancel(ErrStop)
				case syscall.SIGHUP:
					d.Log.Info("received SIGHUP, reloading agent")
					mu.Lock()
					runCancel(ErrReload)
					mu.Unlock()
				}
			case <-done:
				return
			}
		}
	}()

	// Report started and wait for the ack: a successful submit proves the
	// server, auth, and DB are all healthy (this replaces the old Ping) and
	// transitions the task's pending command to running.
	if err := d.emit(emitCtx, model.RunnerEventStarted); err != nil {
		return err
	}

	// Load config
	cfg, err := LoadConfig(d.TaskID)
	if err != nil {
		return d.fail(emitCtx, fmt.Errorf("failed to load config: %w", err))
	}

	d.Log.Info("loaded config",
		"cwd", cfg.Cwd,
		"commands", cfg.Commands,
		"mcp_servers", len(cfg.McpServers),
		"setup_completed", cfg.SetupCommandsCompleted,
		"started", cfg.Started,
	)

	// Setup runs under stopCtx: a reload doesn't interrupt setup commands,
	// only a stop does.
	if err := d.setup(stopCtx, cfg); err != nil {
		if context.Cause(stopCtx) == ErrStop {
			d.Log.Info("agent stopped during setup")
			return d.stop(emitCtx)
		}
		return d.fail(emitCtx, err)
	}

	// Start agent
	a, err := NewAgent(Options{
		Type:       cfg.Type,
		Cwd:        os.ExpandEnv(cfg.Cwd),
		Verbose:    cfg.Verbose,
		McpServers: cfg.McpServers,
		Claude:     cfg.Claude,
		Codex:      cfg.Codex,
		Copilot:    cfg.Copilot,
		Cursor:     cfg.Cursor,
		Sloppy:     cfg.Sloppy,
		Dummy:      cfg.Dummy,
	})
	if err != nil {
		return d.fail(emitCtx, fmt.Errorf("failed to create agent: %w", err))
	}
	defer a.Close()

	// Bootstrap prompt
	prompt, err := cfg.prompt()
	if err != nil {
		return d.fail(emitCtx, fmt.Errorf("failed to build prompt: %w", err))
	}
	resume := cfg.Started

	// A SIGHUP that arrived before the agent ever ran is just a start: swap in
	// a fresh run context and ack the restart command, keeping the original
	// bootstrap prompt since there is no session to reload.
	if context.Cause(runCtx) == ErrReload {
		runCtx = newRun()
		if err := d.emit(emitCtx, model.RunnerEventStarted); err != nil {
			return err
		}
	}

	for {
		err := d.runPrompt(runCtx, a, prompt, resume)
		if errors.Is(err, errUnwindTimeout) {
			return err
		}
		if err == nil {
			// Natural completion wins over any cancel that raced in: a late
			// signal does not discard a genuinely finished run.
			cfg.Started = true
			if err := SaveConfig(d.TaskID, cfg); err != nil {
				return d.fail(emitCtx, fmt.Errorf("failed to save config: %w", err))
			}
			d.Log.Info("task completed successfully")
			return d.stop(emitCtx)
		}
		switch context.Cause(runCtx) {
		case ErrStop:
			d.Log.Info("agent stopped gracefully")
			return d.stop(emitCtx)
		case ErrReload:
			// A stop that arrived during the reload wins.
			if context.Cause(stopCtx) == ErrStop {
				d.Log.Info("agent stopped during reload")
				return d.stop(emitCtx)
			}
			// Re-arm before reporting so a SIGHUP racing in cancels the new
			// run instead of being lost; the extra pass through this branch
			// is a no-op at the server.
			runCtx = newRun()
			if err := d.emit(emitCtx, model.RunnerEventStarted); err != nil {
				return err
			}
			cfg.Started = true
			if prompt, err = cfg.prompt(); err != nil {
				return d.fail(emitCtx, fmt.Errorf("failed to build prompt: %w", err))
			}
			resume = true
			d.Log.Info("agent reloaded")
		default:
			return d.fail(emitCtx, err)
		}
	}
}

// runPrompt invokes a.Prompt and bounds how long the agent may take to unwind
// after ctx is cancelled. If the agent doesn't return within StopTimeout of
// the cancellation, the in-flight Prompt is abandoned and errUnwindTimeout is
// returned.
func (d *Driver) runPrompt(ctx context.Context, a Agent, prompt string, resume bool) error {
	result := make(chan error, 1)
	go func() { result <- a.Prompt(ctx, prompt, resume) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
	}
	select {
	case err := <-result:
		return err
	case <-time.After(StopTimeout):
		return errUnwindTimeout
	}
}

// emit submits a runner event for the driver's task and waits for the ack.
// The SubmitRunnerEvents handler commits the transaction before returning, so
// a nil error means the state transition is durable. Driver events are
// spontaneous and carry version 0 to bypass the server's version check.
func (d *Driver) emit(ctx context.Context, event model.RunnerEventType) error {
	_, err := d.Client.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: d.TaskID, Event: string(event)}},
	})
	if err != nil {
		return fmt.Errorf("failed to submit %s event: %w", event, err)
	}
	return nil
}

// stop reports a graceful stop. The exit code doesn't depend on the ack: if
// the event is lost the driver still exits 0, and the monitor's exit-0
// fallback lands on the same stopped event.
func (d *Driver) stop(ctx context.Context) error {
	if err := d.emit(ctx, model.RunnerEventStopped); err != nil {
		d.Log.Error("failed to report stop", "error", err)
	}
	return nil
}

// fail reports cause as a failed event. An acked submit fulfils the driver's
// reporting duty, so it returns nil and the process exits 0. If the submit
// fails, cause is returned and the non-zero exit lets the monitor's failed
// fallback report instead — exiting 0 there would make the monitor emit
// stopped and silently swallow the failure.
func (d *Driver) fail(ctx context.Context, cause error) error {
	d.Log.Error("task failed", "error", cause)
	if err := d.emit(ctx, model.RunnerEventFailed); err != nil {
		d.Log.Error("failed to report failure", "error", err)
		return cause
	}
	return nil
}

// setup runs the setup commands listed in cfg.Commands, resuming from
// cfg.SetupCommandsCompleted. After each successful command, the updated
// count is persisted via SaveConfig so a restart can pick up where the
// previous run left off.
func (d *Driver) setup(ctx context.Context, cfg *Config) error {
	// Defensive clamp: if the saved count exceeds the current command
	// list, reset to 0 and re-run from the beginning.
	if cfg.SetupCommandsCompleted > len(cfg.Commands) {
		cfg.SetupCommandsCompleted = 0
	}
	for i := cfg.SetupCommandsCompleted; i < len(cfg.Commands); i++ {
		command := cfg.Commands[i]
		d.Log.Info("Running setup command", "index", i, "command", command)
		c := exec.CommandContext(ctx, "sh", "-c", command)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("setup command %d failed: %w", i, err)
		}
		cfg.SetupCommandsCompleted = i + 1
		if err := SaveConfig(d.TaskID, cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	}
	return nil
}

//go:embed PROMPT.md
var promptText string

var promptTemplate = template.Must(template.New("prompt").Parse(promptText))

// prompt builds the bootstrap prompt sent to the agent.
func (c *Config) prompt() (string, error) {
	var b strings.Builder
	err := promptTemplate.Execute(&b, struct {
		Started                 bool
		HasChildTasksCapability bool
		Prompt                  string
	}{
		Started:                 c.Started,
		HasChildTasksCapability: c.hasCapability(agentauth.CapabilityChildTasks),
		Prompt:                  c.Prompt,
	})
	if err != nil {
		return "", err
	}
	return b.String(), nil
}
