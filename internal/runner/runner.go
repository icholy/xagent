package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"

	"connectrpc.com/connect"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/dockerx"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/workspace"
	"github.com/icholy/xagent/internal/xagentclient"
	"golang.org/x/sync/errgroup"
)

const socketPath = "/tmp/xagent.sock"

type Runner struct {
	docker       *client.Client
	client       xagentclient.Client
	proxy        *xagentclient.UnixProxy
	prebuiltDir  string
	workspaces   *workspace.Config
	concurrency  int
	runningCount atomic.Int32
}

type Options struct {
	ServerURL   string
	PrebuiltDir string
	Workspaces  *workspace.Config
	Concurrency int
}

func New(opts Options) (*Runner, error) {
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	p, err := xagentclient.NewUnixProxy(socketPath, opts.ServerURL)
	if err != nil {
		docker.Close()
		return nil, fmt.Errorf("failed to create proxy: %w", err)
	}

	go p.Serve()

	return &Runner{
		docker:      docker,
		client:      xagentclient.New(opts.ServerURL),
		proxy:       p,
		prebuiltDir: opts.PrebuiltDir,
		workspaces:  opts.Workspaces,
		concurrency: opts.Concurrency,
	}, nil
}

func (r *Runner) Close() error {
	r.proxy.Close()
	return r.docker.Close()
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
	resp, err := r.client.ListTasks(ctx, &xagentv1.ListTasksRequest{HasCommand: true})
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(r.concurrency)

	for _, pbTask := range resp.Tasks {
		task := model.TaskFromProto(pbTask)
		switch task.Command {
		case model.TaskCommandStop:
			g.Go(func() error {
				if err := r.kill(ctx, task); err != nil {
					slog.Error("failed to stop task", "task", task.ID, "error", err)
				}
				if err := r.submit(ctx, task.ID, "stopped", task.Version); err != nil {
					slog.Error("failed to send stopped event", "task", task.ID, "error", err)
				}
				return nil
			})
		case model.TaskCommandRestart:
			g.Go(func() error {
				// Kill existing container if running
				if err := r.kill(ctx, task); err != nil {
					slog.Error("failed to kill task for restart", "task", task.ID, "error", err)
				}
				if r.concurrency > 0 && int(r.runningCount.Load()) >= r.concurrency {
					slog.Debug("concurrency limit reached, skipping task", "task", task.ID, "running", r.runningCount.Load(), "limit", r.concurrency)
					return nil
				}
				if err := r.start(ctx, task); err != nil {
					slog.Error("failed to start task", "task", task.ID, "error", err)
					if err := r.submit(ctx, task.ID, "failed", task.Version); err != nil {
						slog.Error("failed to send failed event", "task", task.ID, "error", err)
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
	// Initialize running container count
	runningContainers, err := r.docker.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "xagent=true"),
			filters.Arg("status", "running"),
		),
	})
	if err != nil {
		return fmt.Errorf("failed to list running containers: %w", err)
	}
	r.runningCount.Store(int32(len(runningContainers)))
	slog.Info("initialized running container count", "count", len(runningContainers))

	// Find all exited xagent containers
	containers, err := r.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "xagent=true"), filters.Arg("status", "exited")),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	for _, c := range containers {
		taskIDStr := c.Labels["xagent.task"]
		if taskIDStr == "" {
			continue
		}
		taskID, err := strconv.ParseInt(taskIDStr, 10, 64)
		if err != nil {
			slog.Error("invalid task ID in container label", "task", taskIDStr, "error", err)
			continue
		}

		// Check if task is still running
		task, err := r.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
		if err != nil {
			slog.Error("failed to get task", "task", taskID, "error", err)
			continue
		}
		if task.Task.Status != "running" {
			continue
		}

		// Inspect container to get exit code
		info, err := r.docker.ContainerInspect(ctx, c.ID)
		if err != nil {
			slog.Error("failed to inspect container", "task", taskID, "error", err)
			continue
		}

		// Use version 0 to bypass version check (spontaneous events)
		exitCode := info.State.ExitCode
		if exitCode == 0 {
			slog.Info("reconcile: container exited successfully", "task", taskID)
			if err := r.submit(ctx, taskID, "stopped", 0); err != nil {
				slog.Error("failed to send stopped event", "task", taskID, "error", err)
			}
		} else {
			slog.Error("reconcile: container exited with error", "task", taskID, "exitCode", exitCode)
			if err := r.submit(ctx, taskID, "failed", 0); err != nil {
				slog.Error("failed to send failed event", "task", taskID, "error", err)
			}
		}
	}

	return nil
}

func (r *Runner) kill(ctx context.Context, task *model.Task) error {
	taskIDStr := strconv.FormatInt(task.ID, 10)
	containers, err := r.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "xagent.task="+taskIDStr)),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}
	if len(containers) == 0 {
		return nil
	}
	c := containers[0]
	if c.State != "running" {
		return nil
	}
	slog.Info("killing container", "task", task.ID)
	if err := dockerx.ContainerKill(ctx, r.docker, c.ID, "SIGTERM"); err != nil {
		if errors.Is(err, dockerx.ErrNotRunning) {
			return nil
		}
		return err
	}
	return nil
}

func (r *Runner) create(ctx context.Context, task *model.Task) (string, error) {
	ws, err := r.workspaces.Get(task.Workspace)
	if err != nil {
		return "", fmt.Errorf("failed to get workspace: %w", err)
	}

	// Build container config from workspace
	config, hostConfig, networkConfig := r.buildContainerConfig(task, ws)

	name := fmt.Sprintf("xagent-%d", task.ID)
	slog.Info("creating container", "task", task.ID, "name", name, "image", ws.Container.Image, "workspace", task.Workspace)
	resp, err := r.docker.ContainerCreate(ctx, config, hostConfig, networkConfig, nil, name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	// Copy xagent binary into container
	if err := r.copyBinary(ctx, resp.ID, ws.Container.Image); err != nil {
		return "", fmt.Errorf("failed to copy binary: %w", err)
	}

	// Copy config into container
	if err := r.copyConfig(ctx, resp.ID, task, ws); err != nil {
		return "", fmt.Errorf("failed to copy config: %w", err)
	}

	return resp.ID, nil
}

func (r *Runner) start(ctx context.Context, task *model.Task) error {
	// Look up existing container
	containers, err := r.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", fmt.Sprintf("xagent.task=%d", task.ID))),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	var containerID string
	if len(containers) > 0 {
		c := containers[0]
		slog.Info("starting existing container", "task", task.ID, "name", fmt.Sprintf("xagent-%d", task.ID))
		containerID = c.ID
	} else {
		var err error
		containerID, err = r.create(ctx, task)
		if err != nil {
			return err
		}
	}

	if err := r.docker.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	r.runningCount.Add(1)
	return nil
}

func (r *Runner) buildContainerConfig(task *model.Task, ws *workspace.Workspace) (*container.Config, *container.HostConfig, *network.NetworkingConfig) {
	ctr := &ws.Container
	taskIDStr := strconv.FormatInt(task.ID, 10)

	// Build environment variables
	env := make([]string, 0, len(ctr.Environment)+1)
	env = append(env, "XAGENT_TASK_ID="+taskIDStr)
	for k, v := range ctr.Environment {
		env = append(env, k+"="+v)
	}

	// Build binds (volumes) - always include the socket
	binds := append([]string{socketPath + ":/var/run/xagent.sock"}, ctr.Volumes...)

	config := &container.Config{
		Image: ctr.Image,
		User:  ctr.User,
		Labels: map[string]string{
			"xagent":      "true",
			"xagent.task": taskIDStr,
		},
		Cmd: []string{
			"/usr/local/bin/xagent", "run",
			"--server", "unix:///var/run/xagent.sock",
			"--task", taskIDStr,
		},
		Env:        env,
		WorkingDir: ctr.WorkingDir,
	}

	hostConfig := &container.HostConfig{
		Binds:    binds,
		GroupAdd: ctr.GroupAdd,
	}

	var networkConfig *network.NetworkingConfig
	if len(ctr.Networks) > 0 {
		networkConfig = &network.NetworkingConfig{
			EndpointsConfig: make(map[string]*network.EndpointSettings),
		}
		for _, net := range ctr.Networks {
			networkConfig.EndpointsConfig[net] = &network.EndpointSettings{}
		}
	}

	return config, hostConfig, networkConfig
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

func (r *Runner) copyConfig(ctx context.Context, containerID string, task *model.Task, ws *workspace.Workspace) error {
	taskIDStr := strconv.FormatInt(task.ID, 10)

	// Convert workspace to agent config format
	cfg := agent.Config{
		Type:       ws.Agent.Type,
		Cwd:        ws.Agent.Cwd,
		Prompt:     ws.Agent.Prompt,
		McpServers: make(map[string]agent.McpServer),
		Commands:   ws.Commands,
	}

	// Copy agent-specific config
	if ws.Agent.Claude != nil {
		cfg.Claude = &agent.ClaudeOptions{
			Model: ws.Agent.Claude.Model,
		}
	}
	if ws.Agent.Copilot != nil {
		cfg.Copilot = &agent.CopilotOptions{
			Model: ws.Agent.Copilot.Model,
		}
	}
	if ws.Agent.Cursor != nil {
		cfg.Cursor = &agent.CursorOptions{
			Model: ws.Agent.Cursor.Model,
		}
	}
	if ws.Agent.Dummy != nil {
		cfg.Dummy = &agent.DummyOptions{
			Sleep: ws.Agent.Dummy.Sleep,
		}
	}

	// Inject xagent MCP server for link creation
	cfg.McpServers["xagent"] = agent.McpServer{
		Type:    "stdio",
		Command: "/usr/local/bin/xagent",
		Args:    []string{"mcp", "--mode", "container", "--server", "unix:///var/run/xagent.sock", "--task", taskIDStr, "--workspace", task.Workspace},
	}

	for name, srv := range ws.Agent.McpServers {
		cfg.McpServers[name] = agent.McpServer{
			Type:    srv.Type,
			URL:     srv.URL,
			Headers: srv.Headers,
			Command: srv.Command,
			Args:    srv.Args,
			Env:     srv.Env,
		}
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
				slog.Error("invalid task ID in container event", "task", taskIDStr, "error", err)
				continue
			}

			switch event.Action {
			case events.ActionStart:
				slog.Info("container started", "task", taskID)
				// Use version 0 to bypass version check (spontaneous events)
				if err := r.submit(ctx, taskID, "started", 0); err != nil {
					slog.Error("failed to send started event", "task", taskID, "error", err)
				}
			case events.ActionDie:
				r.runningCount.Add(-1)
				// Use version 0 to bypass version check (spontaneous events)
				exitCode := event.Actor.Attributes["exitCode"]
				if exitCode == "0" {
					slog.Info("container exited successfully", "task", taskID)
					if err := r.submit(ctx, taskID, "stopped", 0); err != nil {
						slog.Error("failed to send stopped event", "task", taskID, "error", err)
					}
				} else {
					slog.Error("container exited with error", "task", taskID, "exitCode", exitCode)
					if err := r.submit(ctx, taskID, "failed", 0); err != nil {
						slog.Error("failed to send failed event", "task", taskID, "error", err)
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
			slog.Error("invalid task ID in container label", "xagent.task", label, "error", err)
			continue
		}
		// Fetch task
		resp, err := r.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
		if err != nil && connect.CodeOf(err) != connect.CodeNotFound {
			slog.Error("failed to get task", "task", taskID, "error", err)
			continue
		}
		task := model.TaskFromProto(resp.Task)

		// Remove container if task is archived or deleted
		if task.Status == model.TaskStatusArchived || connect.CodeOf(err) == connect.CodeNotFound {
			if err := r.docker.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
				slog.Error("failed to remove container", "task", taskID, "error", err)
			} else {
				slog.Info("container removed", "task", taskID)
			}
		}
	}

	return nil
}
