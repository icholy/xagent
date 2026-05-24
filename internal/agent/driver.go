package agent

import (
	"context"
	_ "embed"
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

// reloadTimeout matches the runner's SIGTERM→SIGKILL window. If the
// agent subprocess doesn't exit within this window of receiving
// SIGHUP, the driver bails out non-zero so the runner's docker
// monitor emits `failed` as a fallback.
const reloadTimeout = 30 * time.Second

// emitTimeout caps how long the driver waits for a runner-event ack.
// emit uses context.Background so that SIGTERM-driven cancellation of
// the agent context does not also cancel the in-flight RPC — the ack
// is what proves the state transition is durable.
const emitTimeout = 30 * time.Second

type Driver struct {
	TaskID int64
	Client xagentclient.Client
	Log    *slog.Logger
}

func (d *Driver) Run(ctx context.Context) error {
	// mainCtx is cancelled by SIGTERM with ErrStop. agentCtx (created
	// per iteration below) is a child of mainCtx, cancelled by SIGHUP
	// with ErrReload so reload can recreate it without stopping the
	// driver, while SIGTERM still propagates and takes precedence.
	mainCtx, cancelMain := context.WithCancelCause(ctx)
	defer cancelMain(nil)

	var (
		sigMu       sync.Mutex
		agentCancel context.CancelCauseFunc
		reloading   bool
		reloadTimer *time.Timer
	)
	setAgentCancel := func(c context.CancelCauseFunc) {
		sigMu.Lock()
		defer sigMu.Unlock()
		agentCancel = c
	}
	clearReload := func() {
		sigMu.Lock()
		defer sigMu.Unlock()
		if reloadTimer != nil {
			reloadTimer.Stop()
			reloadTimer = nil
		}
		reloading = false
	}

	sigCh := make(chan os.Signal, 2)
	// PID 1 ignores default-action signals — explicit Notify is mandatory.
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	go func() {
		for sig := range sigCh {
			switch sig {
			case syscall.SIGTERM:
				d.Log.Info("received SIGTERM, stopping agent")
				cancelMain(ErrStop)
			case syscall.SIGHUP:
				sigMu.Lock()
				switch {
				case reloading:
					d.Log.Info("ignoring SIGHUP: reload already in progress")
				case agentCancel == nil:
					d.Log.Info("ignoring SIGHUP: no active agent")
				default:
					d.Log.Info("received SIGHUP, reloading agent")
					reloading = true
					agentCancel(ErrReload)
					reloadTimer = time.AfterFunc(reloadTimeout, func() {
						d.Log.Error("reload timeout exceeded, exiting non-zero")
						os.Exit(1)
					})
				}
				sigMu.Unlock()
			}
		}
	}()

	// Emit `started` before any setup. A successful ack proves the
	// socket, JWT, server, and DB are all healthy — no separate ping.
	if err := d.emit(model.RunnerEventStarted); err != nil {
		return fmt.Errorf("failed to emit started: %w", err)
	}

	cfg, err := LoadConfig(d.TaskID)
	if err != nil {
		return d.fail(fmt.Errorf("failed to load config: %w", err))
	}

	d.Log.Info("loaded config",
		"cwd", cfg.Cwd,
		"commands", cfg.Commands,
		"mcp_servers", len(cfg.McpServers),
		"setup", cfg.Setup,
		"started", cfg.Started,
	)

	if !cfg.Setup {
		for _, command := range cfg.Commands {
			d.Log.Info("Running setup command", "command", command)
			c := exec.CommandContext(mainCtx, "sh", "-c", command)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return d.fail(fmt.Errorf("setup command failed: %w", err))
			}
		}
		cfg.Setup = true
		if err := SaveConfig(d.TaskID, cfg); err != nil {
			return d.fail(fmt.Errorf("failed to save config: %w", err))
		}
	}

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
		return d.fail(fmt.Errorf("failed to create agent: %w", err))
	}
	defer a.Close()

	for {
		prompt, err := cfg.prompt()
		if err != nil {
			return d.fail(fmt.Errorf("failed to build prompt: %w", err))
		}

		agentCtx, agentCancelFn := context.WithCancelCause(mainCtx)
		setAgentCancel(agentCancelFn)
		promptErr := a.Prompt(agentCtx, prompt, cfg.Started)
		agentCancelFn(nil)
		setAgentCancel(nil)

		// SIGTERM wins over SIGHUP — check mainCtx's cause first. We
		// only treat the run as a reload/stop if Prompt actually
		// returned an error; a naturally-completed Prompt is always
		// reported as `stopped`, even if a signal raced in late.
		switch {
		case promptErr != nil && context.Cause(mainCtx) == ErrStop:
			d.Log.Info("agent stopped gracefully")
			if err := d.emit(model.RunnerEventStopped); err != nil {
				return fmt.Errorf("failed to emit stopped: %w", err)
			}
			return nil

		case promptErr != nil && context.Cause(agentCtx) == ErrReload:
			d.Log.Info("agent reloaded")
			// Future iterations must use the resume prompt.
			cfg.Started = true
			if err := SaveConfig(d.TaskID, cfg); err != nil {
				return d.fail(fmt.Errorf("failed to save config: %w", err))
			}
			if err := d.emit(model.RunnerEventStarted); err != nil {
				return fmt.Errorf("failed to emit started: %w", err)
			}
			clearReload()
			continue

		case promptErr != nil:
			return d.fail(promptErr)
		}

		cfg.Started = true
		if err := SaveConfig(d.TaskID, cfg); err != nil {
			return d.fail(fmt.Errorf("failed to save config: %w", err))
		}
		d.Log.Info("Task completed successfully.")
		if err := d.emit(model.RunnerEventStopped); err != nil {
			return fmt.Errorf("failed to emit stopped: %w", err)
		}
		return nil
	}
}

// emit submits a runner event and waits for ack. Driver-emitted events
// use Version: 0 so the server treats them as spontaneous (same
// convention the runner's docker monitor uses).
func (d *Driver) emit(ev model.RunnerEventType) error {
	ctx, cancel := context.WithTimeout(context.Background(), emitTimeout)
	defer cancel()
	_, err := d.Client.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{
			TaskId: d.TaskID,
			Event:  string(ev),
		}},
	})
	if err != nil {
		d.Log.Error("failed to submit runner event", "event", ev, "error", err)
		return err
	}
	d.Log.Info("submitted runner event", "event", ev)
	return nil
}

// fail emits a `failed` event for the given cause. If the emit
// succeeds, the server has already recorded the failure and the
// driver exits 0 — the runner's docker monitor will see a clean exit
// and emit a redundant `stopped` that the state machine ignores. If
// the emit itself fails, fail returns a non-nil error so the driver
// exits non-zero and the monitor's `failed` fallback fires.
func (d *Driver) fail(cause error) error {
	d.Log.Error("agent failed", "error", cause)
	if err := d.emit(model.RunnerEventFailed); err != nil {
		return fmt.Errorf("failed to emit failed (cause: %v): %w", cause, err)
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
		Started            bool
		HasChildTasksScope bool
		Prompt             string
	}{
		Started:            c.Started,
		HasChildTasksScope: c.hasScope(agentauth.ScopeChildTasks),
		Prompt:             c.Prompt,
	})
	if err != nil {
		return "", err
	}
	return b.String(), nil
}
