package runner

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"path"
	"regexp"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/runner/backend"
	"github.com/icholy/xagent/internal/runner/workspace"
	"github.com/icholy/xagent/internal/x/safesem"
	"github.com/icholy/xagent/internal/x/wakeup"
	"github.com/icholy/xagent/internal/xagentclient"
	"golang.org/x/sync/errgroup"
)

type Runner struct {
	backend     backend.Backend
	client      xagentclient.Client
	serverURL   string
	workspaces  *workspace.Config
	runnerID    string
	concurrency int64
	sem         *safesem.Semaphore
	log         *slog.Logger
	queue       *EventQueue
	wake        wakeup.Chan
}

type Options struct {
	Client xagentclient.Client
	// Backend is the sandbox runtime that hosts task drivers.
	Backend backend.Backend
	// ServerURL is the C2 URL injected into sandboxes so the driver and the
	// injected xagent MCP server connect directly to the C2. It is the runner's
	// own configured --server value; the sandbox reaches the same C2 the runner
	// does. The runner authenticates the token-minting RPC with its own xat_ key.
	ServerURL   string
	Workspaces  *workspace.Config
	Concurrency int
	RunnerID    string
	Log         *slog.Logger
	Queue       *EventQueue
}

var reRunnerID = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// ValidateRunnerID checks that id is a valid runner identifier.
func ValidateRunnerID(id string) error {
	if !reRunnerID.MatchString(id) {
		return fmt.Errorf("invalid runner id: %q", id)
	}
	return nil
}

func New(opts Options) (*Runner, error) {
	if err := ValidateRunnerID(opts.RunnerID); err != nil {
		return nil, err
	}
	if opts.Backend == nil {
		return nil, fmt.Errorf("backend is required")
	}

	// Use math.MaxInt64 if no limit is set (concurrency <= 0)
	concurrency := int64(opts.Concurrency)
	if concurrency <= 0 {
		concurrency = math.MaxInt64
	}

	log := cmp.Or(opts.Log, slog.Default())

	return &Runner{
		backend:     opts.Backend,
		client:      opts.Client,
		serverURL:   opts.ServerURL,
		workspaces:  opts.Workspaces,
		runnerID:    opts.RunnerID,
		concurrency: concurrency,
		sem:         safesem.New(concurrency),
		log:         log,
		queue:       opts.Queue,
		wake:        wakeup.New(),
	}, nil
}

// WakeC returns a channel that receives one value per coalesced burst of
// internally-generated wake-ups. Currently signalled when a concurrency
// slot is released so the main loop can immediately retry tasks that were
// previously skipped for being over the limit.
func (r *Runner) WakeC() <-chan struct{} { return r.wake }

// Wake signals the runner's main loop to poll immediately.
func (r *Runner) Wake() { r.wake.Wake() }

func (r *Runner) Close() error {
	return r.backend.Close()
}

// RegisterWorkspaces sends the available workspace names to the server.
func (r *Runner) RegisterWorkspaces(ctx context.Context) error {
	workspaces := make([]*xagentv1.RegisteredWorkspace, 0, len(r.workspaces.Workspaces))
	for name, ws := range r.workspaces.Workspaces {
		workspaces = append(workspaces, &xagentv1.RegisteredWorkspace{
			Name:        name,
			Description: ws.Description,
		})
	}
	_, err := r.client.RegisterWorkspaces(ctx, &xagentv1.RegisterWorkspacesRequest{
		RunnerId:   r.runnerID,
		Workspaces: workspaces,
	})
	if err != nil {
		return fmt.Errorf("failed to register workspaces: %w", err)
	}
	r.log.Info("registered workspaces", "runner_id", r.runnerID, "count", len(workspaces))
	return nil
}

func (r *Runner) Poll(ctx context.Context) error {
	resp, err := r.client.ListRunnerTasks(ctx, &xagentv1.ListRunnerTasksRequest{Runner: r.runnerID})
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(int(r.concurrency))

	for _, pbTask := range resp.Tasks {
		task := model.TaskFromProto(pbTask)
		switch task.Command {
		case model.TaskCommandStop:
			g.Go(func() error {
				signalled, err := r.Kill(ctx, task)
				if err != nil {
					// The stop command survives, so the kill is retried on
					// the next poll.
					r.log.Error("failed to stop task", "task", task.ID, "error", err)
					return nil
				}
				if signalled {
					// The driver owns the terminal report. If it hangs until
					// SIGKILL, the non-zero exit triggers the monitor's
					// "failed" instead.
					return nil
				}
				// No running sandbox to signal: there is no driver to
				// complete the cancel, so emit "stopped" to land the task in
				// cancelled instead of sticking in cancelling.
				r.queue.Enqueue(model.RunnerEvent{
					TaskID:  task.ID,
					Event:   model.RunnerEventStopped,
					Version: task.Version,
				})
				return nil
			})
		case model.TaskCommandRestart:
			g.Go(func() error {
				// Kill existing sandbox if running. The old run's "stopped"
				// is rejected by the status guard while the restart command is
				// pending, so the command survives until the new run's
				// "started" consumes it.
				if _, err := r.Kill(ctx, task); err != nil {
					r.log.Error("failed to kill task for restart", "task", task.ID, "error", err)
				}
				// Atomically acquire a semaphore slot before starting
				if !r.sem.TryAcquire(1) {
					r.log.Debug("concurrency limit reached, skipping task", "task", task.ID, "limit", r.concurrency)
					return nil
				}
				if err := r.Start(ctx, task); err != nil {
					r.sem.Release(1) // Release the slot on failure
					r.log.Error("failed to start task", "task", task.ID, "error", err)
					r.queue.Enqueue(model.RunnerEvent{
						TaskID:  task.ID,
						Event:   model.RunnerEventFailed,
						Version: task.Version,
					})
					return nil
				}
				// The "started" event is submitted by the driver on startup
				return nil
			})
		case model.TaskCommandStart:
			g.Go(func() error {
				// Don't bother checking the backend if the status is still running
				if task.Status == model.TaskStatusRunning {
					r.log.Debug("start command: task.status=running, waiting for it to finish", "task", task.ID)
					return nil
				}
				// Check if the sandbox is already running
				running, err := r.backend.Running(ctx, task.ID)
				if err != nil {
					r.log.Error("failed to check if task is running", "task", task.ID, "error", err)
					return nil
				}
				if running {
					// Sandbox is running - do nothing, let it finish naturally
					// The command will be processed when it stops
					r.log.Debug("start command: sandbox already running, waiting for it to finish", "task", task.ID)
					return nil
				}

				// Sandbox not running - atomically acquire a semaphore slot before starting
				if !r.sem.TryAcquire(1) {
					r.log.Debug("concurrency limit reached, skipping task", "task", task.ID, "limit", r.concurrency)
					return nil
				}
				if err := r.Start(ctx, task); err != nil {
					r.sem.Release(1) // Release the slot on failure
					r.log.Error("failed to start task", "task", task.ID, "error", err)
					r.queue.Enqueue(model.RunnerEvent{
						TaskID:  task.ID,
						Event:   model.RunnerEventFailed,
						Version: task.Version,
					})
					return nil
				}
				// The "started" event is submitted by the driver on startup
				return nil
			})
		}
	}

	return g.Wait()
}

func (r *Runner) Reconcile(ctx context.Context) error {
	sandboxes, err := r.backend.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	// Count already-running sandboxes and set semaphore accordingly.
	// The count can exceed capacity in over-limit scenarios.
	var running int64
	for _, sb := range sandboxes {
		if sb.State == backend.StateRunning {
			running++
		}
	}
	r.sem.Set(running)
	r.log.Info("initialized running sandbox count", "count", running)

	for _, sb := range sandboxes {
		if sb.State != backend.StateExited {
			continue
		}

		// Check if task is still running
		task, err := r.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: sb.TaskID})
		if err != nil {
			r.log.Error("failed to get task", "task", sb.TaskID, "error", err)
			continue
		}
		if task.Task.Status != xagentv1.TaskStatus_RUNNING {
			continue
		}

		// An exited sandbox whose task is still running means the driver's
		// report was lost — "failed" is the honest outcome regardless of exit
		// code (see the driver-owned-events proposal).
		r.log.Error("reconcile: sandbox exited without reporting", "task", sb.TaskID)
		// Use version 0 to bypass version check (spontaneous events)
		r.queue.Enqueue(model.RunnerEvent{
			TaskID: sb.TaskID,
			Event:  model.RunnerEventFailed,
		})
	}

	return nil
}

// Kill stops the task's sandbox if it is running. It reports whether a
// running sandbox was signalled — in that case the driver owns the
// terminal event report (see the driver-owned-events proposal).
func (r *Runner) Kill(ctx context.Context, task *model.Task) (bool, error) {
	return r.backend.Stop(ctx, task.ID)
}

// spec assembles the sandbox spec for a task: the task token, the driver
// invocation, and the agent config with the injected xagent MCP server.
func (r *Runner) spec(ctx context.Context, task *model.Task) (*backend.Spec, error) {
	ws, err := r.workspaces.Get(task.Workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}

	// Mint the task token via the C2 rather than signing it locally: the runner
	// supplies only the task id and capability flags and the server derives the
	// task's workspace/runner/org from the row and signs a narrow app JWT. The
	// driver and injected MCP server present it directly to the C2.
	tokenResp, err := r.client.CreateTaskToken(ctx, &xagentv1.CreateTaskTokenRequest{
		TaskId:       task.ID,
		Capabilities: ws.Capabilities,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create task token: %w", err)
	}
	token := tokenResp.Token

	// Build agent config
	cfg := ws.AgentConfig()
	mcpArgs := []string{
		"tool", "agent-mcp",
		"--server", r.serverURL,
		"--task", fmt.Sprint(task.ID),
		"--runner", task.Runner,
		"--workspace", task.Workspace,
		"--token", token,
	}
	for _, capability := range ws.Capabilities {
		mcpArgs = append(mcpArgs, "--capability", capability)
	}
	cfg.McpServers["xagent"] = agent.McpServer{
		Type:    "stdio",
		Command: backend.BinaryPath,
		Args:    mcpArgs,
	}
	cfgData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	return &backend.Spec{
		TaskID:    task.ID,
		Workspace: ws,
		Cmd: []string{
			backend.BinaryPath, "driver",
			"--server", r.serverURL,
			"--task", fmt.Sprint(task.ID),
			"--token", token,
		},
		Env: []string{
			fmt.Sprintf("XAGENT_TASK_ID=%d", task.ID),
			fmt.Sprintf("XAGENT_TOKEN=%s", token),
			"XAGENT_SERVER=" + r.serverURL,
		},
		Files: []backend.File{
			// Allow non-root agents to write to this directory.
			{Path: path.Dir(agent.ConfigPath(task.ID)), Mode: 0777, Dir: true},
			{Path: agent.ConfigPath(task.ID), Data: cfgData, Mode: 0666},
		},
	}, nil
}

func (r *Runner) Start(ctx context.Context, task *model.Task) error {
	spec, err := r.spec(ctx, task)
	if err != nil {
		return err
	}
	return r.backend.Start(ctx, spec)
}

// Monitor watches for sandbox exits and reports the ones the driver
// couldn't: a non-zero exit means the driver died without reporting its
// outcome, so the runner emits "failed" on its behalf. Exit 0 means the
// driver already reported (see the driver-owned-events proposal), so no
// event is emitted.
func (r *Runner) Monitor(ctx context.Context) error {
	return r.backend.Watch(ctx, func(exit backend.Exit) {
		r.sem.Release(1)
		r.wake.Wake()
		if exit.ExitCode == 0 {
			r.log.Info("sandbox exited, driver already reported", "task", exit.TaskID)
			return
		}
		r.log.Error("sandbox exited without reporting", "task", exit.TaskID, "exitCode", exit.ExitCode)
		// Use version 0 to bypass version check (spontaneous events)
		r.queue.Enqueue(model.RunnerEvent{
			TaskID: exit.TaskID,
			Event:  model.RunnerEventFailed,
		})
	})
}

// Prune removes sandboxes for archived tasks.
func (r *Runner) Prune(ctx context.Context) error {
	sandboxes, err := r.backend.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	// Check each exited sandbox's task status and remove if archived
	for _, sb := range sandboxes {
		if sb.State != backend.StateExited {
			continue
		}
		// Fetch task
		resp, err := r.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: sb.TaskID})
		if err != nil && connect.CodeOf(err) != connect.CodeNotFound {
			r.log.Error("failed to get task", "task", sb.TaskID, "error", err)
			continue
		}
		// Remove sandbox if task is archived or deleted
		if connect.CodeOf(err) == connect.CodeNotFound || resp.Task.Archived {
			if err := r.backend.Remove(ctx, sb.TaskID); err != nil {
				r.log.Error("failed to remove sandbox", "task", sb.TaskID, "error", err)
			} else {
				r.log.Info("sandbox removed", "task", sb.TaskID)
			}
		}
	}

	return nil
}
