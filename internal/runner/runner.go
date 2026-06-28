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
	"sync"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/runner/backend"
	"github.com/icholy/xagent/internal/runner/taskstate"
	"github.com/icholy/xagent/internal/runner/workspace"
	"github.com/icholy/xagent/internal/x/safesem"
	"github.com/icholy/xagent/internal/x/wakeup"
	"github.com/icholy/xagent/internal/xagentclient"
	"golang.org/x/sync/errgroup"
)

type Runner struct {
	backend backend.Backend
	store   *taskstate.Store
	// launchMu serializes a Start's Launch+record-write against Monitor's
	// id→task resolution. A container is created and started inside Launch but
	// its record is written only after Launch returns; without this lock a
	// sandbox that exits in that window delivers its die event to Monitor before
	// the record exists, so store.ByID misses — dropping the terminal event and
	// leaking the concurrency slot. Holding the same lock across Launch+Write
	// and around the watch handler's ByID parks a mid-window exit until the
	// record is committed. (A runner *crash* in that window is the orphan gap
	// the Docker name-conflict adoption in ensure self-heals on the next Start.)
	launchMu    sync.Mutex
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
	// Store is the runner-local source of truth for the task→sandbox-handle
	// mapping. The runner is the only writer; backends never touch it.
	Store *taskstate.Store
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
	if opts.Store == nil {
		return nil, fmt.Errorf("store is required")
	}

	// Use math.MaxInt64 if no limit is set (concurrency <= 0)
	concurrency := int64(opts.Concurrency)
	if concurrency <= 0 {
		concurrency = math.MaxInt64
	}

	log := cmp.Or(opts.Log, slog.Default())

	return &Runner{
		backend:     opts.Backend,
		store:       opts.Store,
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
				running, err := r.Running(ctx, task.ID)
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
	sandboxes, err := r.List(ctx)
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

// handle returns the tracked handle for a task. ok is false when no record
// exists (the runner never started the task, or its sandbox was removed).
func (r *Runner) handle(taskID int64) (backend.Handle, bool, error) {
	rec, ok, err := r.store.Read(taskID)
	if err != nil || !ok {
		return backend.Handle{}, false, err
	}
	return backend.Handle{Type: rec.Type, ID: rec.ID, Data: rec.Data}, true, nil
}

// Running reports whether the task's sandbox is currently running, probing the
// handle the store tracks for it.
func (r *Runner) Running(ctx context.Context, taskID int64) (bool, error) {
	h, ok, err := r.handle(taskID)
	if err != nil || !ok {
		return false, err
	}
	state, err := r.backend.Probe(ctx, h)
	if err != nil {
		return false, err
	}
	return state == backend.StateRunning, nil
}

// List composes the orchestrator's sandbox view from the store: one Sandbox
// per tracked record, its state resolved by probing the backend handle.
func (r *Runner) List(ctx context.Context) ([]backend.Sandbox, error) {
	records, err := r.store.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list records: %w", err)
	}
	sandboxes := make([]backend.Sandbox, 0, len(records))
	for _, rec := range records {
		state, err := r.backend.Probe(ctx, backend.Handle{Type: rec.Type, ID: rec.ID, Data: rec.Data})
		if err != nil {
			return nil, fmt.Errorf("failed to probe task %d: %w", rec.TaskID, err)
		}
		sandboxes = append(sandboxes, backend.Sandbox{TaskID: rec.TaskID, State: state})
	}
	return sandboxes, nil
}

// Remove destroys the task's sandbox and deletes its record. The record is
// removed only after the backend destroy succeeds, so a failed destroy leaves
// the task tracked and the removal is retried.
func (r *Runner) Remove(ctx context.Context, taskID int64) error {
	h, ok, err := r.handle(taskID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err := r.backend.Destroy(ctx, h); err != nil {
		return err
	}
	return r.store.Remove(taskID)
}

// Kill stops the task's sandbox if it is running. It reports whether a
// running sandbox was signalled — in that case the driver owns the
// terminal event report (see the driver-owned-events proposal).
func (r *Runner) Kill(ctx context.Context, task *model.Task) (bool, error) {
	h, ok, err := r.handle(task.ID)
	if err != nil || !ok {
		return false, err
	}
	return r.backend.Signal(ctx, h)
}

// spec assembles the sandbox spec for a task: the task token, the driver
// invocation, and the agent config with the injected xagent MCP server.
func (r *Runner) spec(ctx context.Context, task *model.Task) (*backend.Spec, error) {
	ws, err := r.workspaces.Get(task.Workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}
	if err := r.backend.ValidateWorkspace(ws); err != nil {
		return nil, fmt.Errorf("invalid workspace %q: %w", task.Workspace, err)
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

// Start ensures the task's sandbox is running and records its handle. It is
// idempotent: if the tracked handle is already running it returns without
// relaunching. Otherwise the prior handle (if any) is passed to Launch so the
// backend can adopt or clean up the existing sandbox, and the returned handle
// is persisted — the store write is the runner's job, not the backend's.
func (r *Runner) Start(ctx context.Context, task *model.Task) error {
	// Resolve the tracked handle first: a still-running sandbox makes Start a
	// no-op, and we skip minting a token for it. An exited handle is passed to
	// Launch so the backend can adopt or clean up the existing sandbox.
	var reuse *backend.Handle
	if h, ok, err := r.handle(task.ID); err != nil {
		return err
	} else if ok {
		state, err := r.backend.Probe(ctx, h)
		if err != nil {
			return err
		}
		if state == backend.StateRunning {
			return nil
		}
		reuse = &h
	}

	spec, err := r.spec(ctx, task)
	if err != nil {
		return err
	}

	// Launch and record the handle atomically with respect to Monitor: the
	// container starts inside Launch, so its die event must not be able to reach
	// Monitor's store.ByID before the record exists. See launchMu.
	r.launchMu.Lock()
	defer r.launchMu.Unlock()

	h, err := r.backend.Launch(ctx, spec, reuse)
	if err != nil {
		return err
	}
	if err := r.store.Write(taskstate.Record{
		TaskID: task.ID,
		Type:   h.Type,
		ID:     h.ID,
		Data:   h.Data,
	}); err != nil {
		return fmt.Errorf("failed to record task handle: %w", err)
	}
	return nil
}

// Monitor watches for sandbox exits and reports the ones the driver
// couldn't: a non-zero exit means the driver died without reporting its
// outcome, so the runner emits "failed" on its behalf. Exit 0 means the
// driver already reported (see the driver-owned-events proposal), so no
// event is emitted.
func (r *Runner) Monitor(ctx context.Context) error {
	return r.backend.Watch(ctx, func(exit backend.HandleExit) {
		// Resolve the handle id back to a task. Take launchMu so an exit that
		// fires while a Start is mid-Launch parks here until that Start commits
		// its record, closing the race where ByID would miss a just-started
		// container. Ids the store doesn't track aren't ours (or were already
		// removed), so ignore them — no slot was held and no task event is owed.
		r.launchMu.Lock()
		rec, ok, err := r.store.ByID(exit.ID)
		r.launchMu.Unlock()
		if err != nil {
			r.log.Error("failed to resolve sandbox exit", "id", exit.ID, "error", err)
			return
		}
		if !ok {
			r.log.Debug("ignoring exit for untracked sandbox", "id", exit.ID)
			return
		}

		r.sem.Release(1)
		r.wake.Wake()
		if exit.ExitCode == 0 {
			r.log.Info("sandbox exited, driver already reported", "task", rec.TaskID)
			return
		}
		r.log.Error("sandbox exited without reporting", "task", rec.TaskID, "exitCode", exit.ExitCode)
		// Use version 0 to bypass version check (spontaneous events)
		r.queue.Enqueue(model.RunnerEvent{
			TaskID: rec.TaskID,
			Event:  model.RunnerEventFailed,
		})
	})
}

// Prune removes sandboxes for archived tasks.
func (r *Runner) Prune(ctx context.Context) error {
	sandboxes, err := r.List(ctx)
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
			if err := r.Remove(ctx, sb.TaskID); err != nil {
				r.log.Error("failed to remove sandbox", "task", sb.TaskID, "error", err)
			} else {
				r.log.Info("sandbox removed", "task", sb.TaskID)
			}
		}
	}

	return nil
}
