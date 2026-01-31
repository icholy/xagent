package runner

import (
	"archive/tar"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"connectrpc.com/connect"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/agentauth"
	"github.com/icholy/xagent/internal/dockerx"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/safesem"
	"github.com/icholy/xagent/internal/workspace"
	"github.com/icholy/xagent/internal/xagentclient"
	"golang.org/x/sync/errgroup"
)

type Runner struct {
	docker      *client.Client
	client      xagentclient.Client
	proxy       *AgentProxy
	prebuiltDir string
	workspaces  *workspace.Config
	runnerID    string
	concurrency int64
	sem         *safesem.Semaphore
	log         *slog.Logger
}

type Options struct {
	ServerURL   string
	PrebuiltDir string
	SecretFile  string
	Workspaces  *workspace.Config
	Concurrency int
	RunnerID    string
	Log         *slog.Logger
	Auth        xagentclient.TokenSource
	SocketPath  string // defaults to /tmp/xagent.sock
}

func New(opts Options) (*Runner, error) {
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	// Load or create private key
	privateKey, err := agentauth.LoadOrCreatePrivateKey(opts.SecretFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load private key: %w", err)
	}

	// Use math.MaxInt64 if no limit is set (concurrency <= 0)
	concurrency := int64(opts.Concurrency)
	if concurrency <= 0 {
		concurrency = math.MaxInt64
	}

	log := cmp.Or(opts.Log, slog.Default())

	proxy := NewProxy(AgentProxyOptions{
		ServerURL:  opts.ServerURL,
		Auth:       opts.Auth,
		PrivateKey: privateKey,
		Log:        log,
		SocketPath: opts.SocketPath,
	})
	if err := proxy.Start(); err != nil {
		return nil, fmt.Errorf("failed to start proxy: %w", err)
	}

	return &Runner{
		docker:      docker,
		client:      xagentclient.New(xagentclient.Options{BaseURL: opts.ServerURL, Source: opts.Auth}),
		proxy:       proxy,
		prebuiltDir: opts.PrebuiltDir,
		workspaces:  opts.Workspaces,
		runnerID:    opts.RunnerID,
		concurrency: concurrency,
		sem:         safesem.New(concurrency),
		log:         log,
	}, nil
}

func (r *Runner) Close() error {
	return errors.Join(
		r.proxy.Close(),
		r.docker.Close(),
	)
}

// RegisterWorkspaces sends the available workspace names to the server.
func (r *Runner) RegisterWorkspaces(ctx context.Context) error {
	workspaces := make([]*xagentv1.RegisteredWorkspace, 0, len(r.workspaces.Workspaces))
	for name := range r.workspaces.Workspaces {
		workspaces = append(workspaces, &xagentv1.RegisteredWorkspace{Name: name})
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

func (r *Runner) submit(ctx context.Context, taskID int64, event string, version int64) error {
	_, err := r.client.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: taskID, Event: event, Version: version},
		},
	})
	return err
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
				if err := r.Kill(ctx, task); err != nil {
					r.log.Error("failed to stop task", "task", task.ID, "error", err)
				}
				if err := r.submit(ctx, task.ID, "stopped", task.Version); err != nil {
					r.log.Error("failed to send stopped event", "task", task.ID, "error", err)
				}
				return nil
			})
		case model.TaskCommandRestart:
			g.Go(func() error {
				// Kill existing container if running
				if err := r.Kill(ctx, task); err != nil {
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
					if err := r.submit(ctx, task.ID, "failed", task.Version); err != nil {
						r.log.Error("failed to send failed event", "task", task.ID, "error", err)
					}
					return nil
				}
				// The "started" event is sent by Monitor when the container starts
				return nil
			})
		case model.TaskCommandStart:
			g.Go(func() error {
				// Don't bother checking docker if the status is still running
				if task.Status == model.TaskStatusRunning {
					r.log.Debug("start command: task.status=running, waiting for it to finish", "task", task.ID)
					return nil
				}
				// Check if container is already running
				running, err := r.isRunning(ctx, task)
				if err != nil {
					r.log.Error("failed to check if task is running", "task", task.ID, "error", err)
					return nil
				}
				if running {
					// Container is running - do nothing, let it finish naturally
					// The command will be processed when it stops
					r.log.Debug("start command: container already running, waiting for it to finish", "task", task.ID)
					return nil
				}

				// Container not running - atomically acquire a semaphore slot before starting
				if !r.sem.TryAcquire(1) {
					r.log.Debug("concurrency limit reached, skipping task", "task", task.ID, "limit", r.concurrency)
					return nil
				}
				if err := r.Start(ctx, task); err != nil {
					r.sem.Release(1) // Release the slot on failure
					r.log.Error("failed to start task", "task", task.ID, "error", err)
					if err := r.submit(ctx, task.ID, "failed", task.Version); err != nil {
						r.log.Error("failed to send failed event", "task", task.ID, "error", err)
					}
					return nil
				}
				// The "started" event is sent by Monitor when the container starts
				return nil
			})
		}
	}

	return g.Wait()
}

func (r *Runner) Reconcile(ctx context.Context) error {
	// Count already-running containers and set semaphore accordingly
	runningContainers, err := r.docker.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "xagent=true"),
			filters.Arg("status", "running"),
		),
	})
	if err != nil {
		return fmt.Errorf("failed to list running containers: %w", err)
	}
	runningCount := int64(len(runningContainers))
	// Set count to running containers (can exceed capacity in over-limit scenarios)
	r.sem.Set(runningCount)
	r.log.Info("initialized running container count", "count", runningCount)

	// Find all exited xagent containers
	containers, err := r.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "xagent=true"), filters.Arg("status", "exited")),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	for _, c := range containers {
		label := c.Labels["xagent.task"]
		if label == "" {
			continue
		}
		taskID, err := strconv.ParseInt(label, 10, 64)
		if err != nil {
			r.log.Error("invalid task ID in container label", "task", label, "error", err)
			continue
		}

		// Check if task is still running
		task, err := r.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
		if err != nil {
			r.log.Error("failed to get task", "task", taskID, "error", err)
			continue
		}
		if task.Task.Status != "running" {
			continue
		}

		// Inspect container to get exit code
		info, err := r.docker.ContainerInspect(ctx, c.ID)
		if err != nil {
			r.log.Error("failed to inspect container", "task", taskID, "error", err)
			continue
		}

		// Use version 0 to bypass version check (spontaneous events)
		exitCode := info.State.ExitCode
		if exitCode == 0 {
			r.log.Info("reconcile: container exited successfully", "task", taskID)
			if err := r.submit(ctx, taskID, "stopped", 0); err != nil {
				r.log.Error("failed to send stopped event", "task", taskID, "error", err)
			}
		} else {
			r.log.Error("reconcile: container exited with error", "task", taskID, "exitCode", exitCode)
			if err := r.submit(ctx, taskID, "failed", 0); err != nil {
				r.log.Error("failed to send failed event", "task", taskID, "error", err)
			}
		}
	}

	return nil
}

// find returns the container for the given task.
// Returns (container, true, nil) if found, (empty, false, nil) if not found,
// or (empty, false, error) on error.
func (r *Runner) find(ctx context.Context, taskID int64) (container.Summary, bool, error) {
	containers, err := r.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", fmt.Sprintf("xagent.task=%d", taskID))),
	})
	if err != nil {
		return container.Summary{}, false, fmt.Errorf("failed to list containers: %w", err)
	}
	if len(containers) == 0 {
		return container.Summary{}, false, nil
	}
	return containers[0], true, nil
}

func (r *Runner) isRunning(ctx context.Context, task *model.Task) (bool, error) {
	c, ok, err := r.find(ctx, task.ID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return c.State == "running", nil
}

func (r *Runner) Kill(ctx context.Context, task *model.Task) error {
	c, ok, err := r.find(ctx, task.ID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if c.State != "running" {
		return nil
	}
	r.log.Info("killing container", "task", task.ID)

	// Try SIGTERM first with a timeout
	killCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = dockerx.ContainerKill(killCtx, r.docker, c.ID, "SIGTERM")
	if err == nil || errors.Is(err, dockerx.ErrNotRunning) {
		return nil
	}

	// If SIGTERM timed out, send SIGKILL
	if errors.Is(err, context.DeadlineExceeded) {
		r.log.Warn("SIGTERM timed out, sending SIGKILL", "task", task.ID)
		if err := dockerx.ContainerKill(ctx, r.docker, c.ID, "SIGKILL"); err != nil {
			if errors.Is(err, dockerx.ErrNotRunning) {
				return nil
			}
			return err
		}
		return nil
	}

	return err
}

func (r *Runner) create(ctx context.Context, task *model.Task) (string, error) {
	ws, err := r.workspaces.Get(task.Workspace)
	if err != nil {
		return "", fmt.Errorf("failed to get workspace: %w", err)
	}

	// Generate JWT for this task
	token, err := r.proxy.TaskToken(task)
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	wc := &ws.Container

	name := fmt.Sprintf("xagent-%d", task.ID)
	r.log.Info("creating container", "task", task.ID, "name", name, "image", wc.Image, "workspace", task.Workspace)

	resp, err := r.docker.ContainerCreate(ctx,
		&container.Config{
			Image: wc.Image,
			User:  wc.User,
			Labels: map[string]string{
				"xagent":      "true",
				"xagent.task": fmt.Sprint(task.ID),
			},
			Cmd: []string{
				"/usr/local/bin/xagent", "run",
				"--server", "unix:///var/run/xagent.sock",
				"--task", fmt.Sprint(task.ID),
				"--token", token,
			},
			Env:        append(wc.Environ(), fmt.Sprintf("XAGENT_TASK_ID=%d", task.ID)),
			WorkingDir: wc.WorkingDir,
		},
		&container.HostConfig{
			Binds:    append([]string{r.proxy.SocketPath() + ":/var/run/xagent.sock"}, wc.Volumes...),
			GroupAdd: wc.GroupAdd,
			Runtime:  wc.Runtime,
		},
		wc.NetworkingConfig(),
		nil,
		name,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	// Copy xagent binary into container
	if err := r.copyBinary(ctx, resp.ID, wc.Image); err != nil {
		return "", fmt.Errorf("failed to copy binary: %w", err)
	}

	// Copy config into container
	if err := r.copyConfig(ctx, resp.ID, task, ws, token); err != nil {
		return "", fmt.Errorf("failed to copy config: %w", err)
	}

	return resp.ID, nil
}

func (r *Runner) Start(ctx context.Context, task *model.Task) error {
	c, ok, err := r.find(ctx, task.ID)
	if err != nil {
		return err
	}

	var containerID string
	if ok {
		r.log.Info("starting existing container", "task", task.ID, "name", fmt.Sprintf("xagent-%d", task.ID))
		containerID = c.ID
	} else {
		containerID, err = r.create(ctx, task)
		if err != nil {
			return err
		}
	}

	if err := r.docker.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	return nil
}

func (r *Runner) copyBinary(ctx context.Context, containerID, image string) error {
	// Inspect image to get architecture
	info, err := r.docker.ImageInspect(ctx, image)
	if err != nil {
		return fmt.Errorf("failed to inspect image: %w", err)
	}

	arch := info.Architecture
	binPath := filepath.Join(r.prebuiltDir, fmt.Sprintf("xagent-linux-%s", arch))

	// Read the binary
	binData, err := os.ReadFile(binPath)
	if err != nil {
		return fmt.Errorf("failed to read binary %s: %w", binPath, err)
	}

	// Create tar archive
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: "xagent",
		Mode: 0755,
		Size: int64(len(binData)),
	}); err != nil {
		return err
	}
	if _, err := tw.Write(binData); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}

	// Copy to container
	return r.docker.CopyToContainer(ctx, containerID, "/usr/local/bin", &buf, container.CopyToContainerOptions{})
}

func (r *Runner) copyConfig(ctx context.Context, containerID string, task *model.Task, ws *workspace.Workspace, token string) error {
	taskIDStr := strconv.FormatInt(task.ID, 10)

	cfg := ws.AgentConfig()

	// Inject xagent MCP server for link creation
	cfg.McpServers["xagent"] = agent.McpServer{
		Type:    "stdio",
		Command: "/usr/local/bin/xagent",
		Args: []string{
			"mcp",
			"--server", "unix:///var/run/xagent.sock",
			"--task", taskIDStr,
			"--runner", task.Runner,
			"--workspace", task.Workspace,
			"--token", token,
		},
	}

	data, err := cfg.Tar(taskIDStr)
	if err != nil {
		return err
	}

	return r.docker.CopyToContainer(ctx, containerID, "/", bytes.NewReader(data), container.CopyToContainerOptions{})
}

// Monitor watches for container starts and exits and sends runner events accordingly.
func (r *Runner) Monitor(ctx context.Context) error {
	eventCh, errCh := r.docker.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("type", "container"),
			filters.Arg("event", "start"),
			filters.Arg("event", "die"),
			filters.Arg("label", "xagent=true"),
		),
	})

	for {
		select {
		case event := <-eventCh:
			taskIDStr := event.Actor.Attributes["xagent.task"]
			taskID, err := strconv.ParseInt(taskIDStr, 10, 64)
			if err != nil {
				r.log.Error("invalid task ID in container event", "task", taskIDStr, "error", err)
				continue
			}

			switch event.Action {
			case events.ActionStart:
				r.log.Info("container started", "task", taskID)
				// Use version 0 to bypass version check (spontaneous events)
				if err := r.submit(ctx, taskID, "started", 0); err != nil {
					r.log.Error("failed to send started event", "task", taskID, "error", err)
				}
			case events.ActionDie:
				r.sem.Release(1)
				// Use version 0 to bypass version check (spontaneous events)
				exitCode := event.Actor.Attributes["exitCode"]
				if exitCode == "0" {
					r.log.Info("container exited successfully", "task", taskID)
					if err := r.submit(ctx, taskID, "stopped", 0); err != nil {
						r.log.Error("failed to send stopped event", "task", taskID, "error", err)
					}
				} else {
					r.log.Error("container exited with error", "task", taskID, "exitCode", exitCode)
					if err := r.submit(ctx, taskID, "failed", 0); err != nil {
						r.log.Error("failed to send failed event", "task", taskID, "error", err)
					}
				}
			}

		case err := <-errCh:
			return fmt.Errorf("docker events error: %w", err)

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Prune removes containers for archived tasks.
func (r *Runner) Prune(ctx context.Context) error {
	// List all stopped xagent containers
	containers, err := r.docker.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", "xagent=true"),
			filters.Arg("status", "exited"),
		),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Check each container's task status and remove if archived
	for _, c := range containers {
		label := c.Labels["xagent.task"]
		if label == "" {
			continue
		}
		taskID, err := strconv.ParseInt(label, 10, 64)
		if err != nil {
			r.log.Error("invalid task ID in container label", "xagent.task", label, "error", err)
			continue
		}
		// Fetch task
		resp, err := r.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
		if err != nil && connect.CodeOf(err) != connect.CodeNotFound {
			r.log.Error("failed to get task", "task", taskID, "error", err)
			continue
		}
		// Remove container if task is archived or deleted
		if connect.CodeOf(err) == connect.CodeNotFound || resp.Task.Archived {
			if err := r.docker.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
				r.log.Error("failed to remove container", "task", taskID, "error", err)
			} else {
				r.log.Info("container removed", "task", taskID)
			}
		}
	}

	return nil
}
