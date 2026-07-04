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
	"syscall"
	"text/template"

	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/shell"
	"github.com/icholy/xagent/internal/xagentclient"
)

type Driver struct {
	TaskID int64
	Client xagentclient.Client
	Log    *slog.Logger

	// ServerURL and Token are the driver's own server credentials, reused to dial
	// the shell relay WebSocket when the task is a debug-shell run. They mirror
	// the values passed to xagentclient.New for Client above.
	ServerURL string
	Token     string
}

// Run executes the task and reports its outcome to the server. The driver
// owns the started / stopped / failed runner events (see the
// driver-owned-events proposal); it returns an error only when it could not
// report the outcome itself, so the runner's monitor reads the exit code as
// a single bit meaning "did the driver report?".
func (d *Driver) Run(ctx context.Context) error {
	// Events are submitted on the parent context, which survives the SIGTERM
	// cancellation below — the terminal events go out after the run context
	// is torn down, bounded by the client's HTTP timeout.
	eventCtx := ctx

	// Set up SIGTERM handler to cancel with ErrStop
	ctx, cancel := context.WithCancelCause(ctx)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	go func() {
		<-sigCh
		d.Log.Info("received SIGTERM, stopping agent")
		cancel(ErrStop)
	}()
	defer signal.Stop(sigCh)

	// Report started: replaces the startup ping — an acked submit proves the
	// connection, token, server, and DB are all healthy.
	if err := d.submit(eventCtx, model.RunnerEventStarted); err != nil {
		return err
	}

	err := d.run(ctx)
	if err != nil && context.Cause(ctx) == ErrStop {
		d.Log.Info("agent stopped gracefully")
		err = nil
	}
	event := model.RunnerEventStopped
	if err != nil {
		d.Log.Error("task failed", "err", err)
		event = model.RunnerEventFailed
	}
	// The terminal ack decides the exit code: acked means the outcome is
	// durably recorded and the driver exits 0 even on agent failure; a lost
	// report exits non-zero so the monitor's "failed" fires.
	if serr := d.submit(eventCtx, event); serr != nil {
		return errors.Join(err, serr)
	}
	return nil
}

// submit reports a runner event for the driver's task and waits for the ack.
// The SubmitRunnerEvents handler commits the transaction before returning,
// so a nil error means the state transition is durable.
func (d *Driver) submit(ctx context.Context, event model.RunnerEventType) error {
	// Version 0 bypasses the version check (spontaneous events).
	re := model.RunnerEvent{TaskID: d.TaskID, Event: event}
	_, err := d.Client.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{re.Proto()},
	})
	if err != nil {
		return fmt.Errorf("failed to submit %s event: %w", event, err)
	}
	return nil
}

// run reads the task and forks into one of two mutually exclusive modes: a
// debug-shell run when the task carries a shell_session, or the normal agent
// path otherwise. A sandbox run is one mode, chosen once at startup (see the
// design in proposals/draft/driver-reverse-shell.md). The fork lives inside run
// so a shell run emits the same started/stopped/failed lifecycle events as an
// agent run.
func (d *Driver) run(ctx context.Context) error {
	resp, err := d.Client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: d.TaskID})
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}
	if session := resp.GetTask().GetShellSession(); session != "" {
		return shell.Serve(ctx, shell.ServeOptions{
			ServerURL: d.ServerURL,
			Token:     d.Token,
			Session:   session,
			Log:       d.Log,
		})
	}
	return d.runAgent(ctx)
}

// runAgent executes the setup commands and the agent prompt. This is the normal
// (non-shell) sandbox run.
func (d *Driver) runAgent(ctx context.Context) error {
	// Load config
	cfg, err := LoadConfig(d.TaskID)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	d.Log.Info("loaded config",
		"cwd", cfg.Cwd,
		"commands", cfg.Commands,
		"mcp_servers", len(cfg.McpServers),
		"setup_completed", cfg.SetupCommandsCompleted,
		"started", cfg.Started,
	)

	if err := d.setup(ctx, cfg); err != nil {
		return err
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
		return fmt.Errorf("failed to create agent: %w", err)
	}
	defer a.Close()

	// Bootstrap prompt
	prompt, err := cfg.prompt()
	if err != nil {
		return fmt.Errorf("failed to build prompt: %w", err)
	}

	if err := a.Prompt(ctx, prompt, cfg.Started); err != nil {
		return err
	}

	// Save config
	cfg.Started = true
	if err := SaveConfig(d.TaskID, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	d.Log.Info("Task completed successfully.")
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
		Started bool
		Prompt  string
	}{
		Started: c.Started,
		Prompt:  c.Prompt,
	})
	if err != nil {
		return "", err
	}
	return b.String(), nil
}
