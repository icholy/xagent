package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/icholy/xagent/internal/agent"
	"github.com/icholy/xagent/internal/notify"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/workspace"
	"github.com/icholy/xagent/internal/xagentclient"
)

const socketPath = "/tmp/xagent.sock"

type Runner struct {
	docker       *client.Client
	client       xagentclient.Client
	proxy        *xagentclient.UnixProxy
	prebuiltDir  string
	workspaces   *workspace.Config
	debug        bool
	concurrency  int
	notify       bool
	runningCount atomic.Int32
}

type Options struct {
	ServerURL   string
	PrebuiltDir string
	Workspaces  *workspace.Config
	Debug       bool
	Concurrency int
	Notify      bool
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
		debug:       opts.Debug,
		concurrency: opts.Concurrency,
		notify:      opts.Notify,
	}, nil
}

func (r *Runner) Close() error {
	r.proxy.Close()
	return r.docker.Close()
}

func (r *Runner) log(ctx context.Context, taskID int64, typ, content string) {
	_, err := r.client.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
		TaskId: taskID,
		Entries: []*xagentv1.LogEntry{
			{Type: typ, Content: content},
		},
	})
	if err != nil {
		slog.Error("failed to upload log", "task", taskID, "error", err)
	}
}

func (r *Runner) taskDisplayName(ctx context.Context, taskID int64) string {
	resp, err := r.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
	if err != nil {
		return fmt.Sprintf("Task %d", taskID)
	}
	if resp.Task.Name == "" {
		return fmt.Sprintf("Task %d", taskID)
	}
	return fmt.Sprintf("%q", resp.Task.Name)
}

func (r *Runner) Poll(ctx context.Context) error {
	resp, err := r.client.ListTasks(ctx, &xagentv1.ListTasksRequest{Statuses: []string{"pending", "cancelled", "restarting"}})
	if err != nil {
		return err
	}

	for _, task := range resp.Tasks {
		switch task.Status {
		case "cancelled":
			if err := r.killTask(ctx, task); err != nil {
				slog.Error("failed to cancel task", "task", task.Id, "error", err)
			} else {
				r.log(ctx, task.Id, "info", "task cancelled, container killed")
			}
		case "restarting":
			r.log(ctx, task.Id, "info", "container restarting")
			if err := r.killTask(ctx, task); err != nil {
				slog.Error("failed to kill task for restart", "task", task.Id, "error", err)
			}
			if r.concurrency > 0 && int(r.runningCount.Load()) >= r.concurrency {
				slog.Debug("concurrency limit reached, skipping restarting task", "task", task.Id, "running", r.runningCount.Load(), "limit", r.concurrency)
				continue
			}
			if err := r.startTask(ctx, task); err != nil {
				slog.Error("failed to restart task", "task", task.Id, "error", err)
				r.log(ctx, task.Id, "error", fmt.Sprintf("failed to restart task: %v", err))
				if _, err := r.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: task.Id, Status: "failed"}); err != nil {
					slog.Error("failed to update task status", "task", task.Id, "error", err)
				}
				continue
			}
			if _, err := r.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: task.Id, Status: "running"}); err != nil {
				slog.Error("failed to update task status", "task", task.Id, "error", err)
			}
		case "pending":
			if r.concurrency > 0 && int(r.runningCount.Load()) >= r.concurrency {
				slog.Debug("concurrency limit reached, skipping pending task", "task", task.Id, "running", r.runningCount.Load(), "limit", r.concurrency)
				continue
			}
			r.log(ctx, task.Id, "info", "container starting")
			if err := r.startTask(ctx, task); err != nil {
				slog.Error("failed to start container", "task", task.Id, "error", err)
				r.log(ctx, task.Id, "error", fmt.Sprintf("failed to start container: %v", err))
				if _, err := r.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: task.Id, Status: "failed"}); err != nil {
					slog.Error("failed to update task status", "task", task.Id, "error", err)
				}
				continue
			}
			if _, err := r.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: task.Id, Status: "running"}); err != nil {
				slog.Error("failed to update task status", "task", task.Id, "error", err)
			}
		}
	}

	return nil
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

		exitCode := info.State.ExitCode
		if exitCode == 0 {
			slog.Info("reconcile: container exited successfully", "task", taskID)
			r.log(ctx, taskID, "info", "container exited successfully (reconciled)")
			if _, err := r.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: taskID, Status: "completed"}); err != nil {
				slog.Error("failed to update task status", "task", taskID, "error", err)
			}
		} else {
			slog.Error("reconcile: container exited with error", "task", taskID, "exitCode", exitCode)
			r.log(ctx, taskID, "error", fmt.Sprintf("container exited with code %d (reconciled)", exitCode))
			if _, err := r.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: taskID, Status: "failed"}); err != nil {
				slog.Error("failed to update task status", "task", taskID, "error", err)
			}
		}
	}

	return nil
}

func (r *Runner) killTask(ctx context.Context, task *xagentv1.Task) error {
	taskIDStr := strconv.FormatInt(task.Id, 10)
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
	if c.State == "running" {
		slog.Info("killing cancelled task container", "task", task.Id)
		if err := r.docker.ContainerKill(ctx, c.ID, "SIGKILL"); err != nil {
			return fmt.Errorf("failed to kill container: %w", err)
		}
		// Wait for the container to actually exit
		waitCh, errCh := r.docker.ContainerWait(ctx, c.ID, container.WaitConditionNotRunning)
		select {
		case <-waitCh:
			// Container has exited
		case err := <-errCh:
			return fmt.Errorf("failed to wait for container to exit: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (r *Runner) startTask(ctx context.Context, task *xagentv1.Task) error {
	ws, err := r.workspaces.Get(task.Workspace)
	if err != nil {
		return fmt.Errorf("failed to get workspace: %w", err)
	}

	taskIDStr := strconv.FormatInt(task.Id, 10)
	containerName := "xagent-" + taskIDStr

	// Look up existing container
	containers, err := r.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "xagent.task="+taskIDStr)),
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	var containerID string
	if len(containers) > 0 {
		c := containers[0]
		if c.State == "running" {
			slog.Info("container already running", "task", task.Id, "container", containerName)
			return nil
		}
		slog.Info("starting existing container", "task", task.Id, "container", containerName)
		containerID = c.ID

		// In debug mode, copy fresh binary each time
		if r.debug {
			if err := r.copyBinary(ctx, containerID, ws.Container.Image); err != nil {
				return fmt.Errorf("failed to copy binary: %w", err)
			}
		}
	} else {
		// Build container config from workspace
		config, hostConfig, networkConfig := r.buildContainerConfig(task, ws)

		slog.Info("creating container", "task", task.Id, "container", containerName, "image", ws.Container.Image, "workspace", task.Workspace)
		r.log(ctx, task.Id, "info", fmt.Sprintf("creating container %s with image %s", containerName, ws.Container.Image))
		resp, err := r.docker.ContainerCreate(ctx, config, hostConfig, networkConfig, nil, containerName)
		if err != nil {
			return fmt.Errorf("failed to create container: %w", err)
		}

		// Copy xagent binary into container
		if err := r.copyBinary(ctx, resp.ID, ws.Container.Image); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}

		// Copy config into container
		if err := r.copyConfig(ctx, resp.ID, task, ws); err != nil {
			return fmt.Errorf("failed to copy config: %w", err)
		}

		containerID = resp.ID
	}

	if err := r.docker.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	r.runningCount.Add(1)

	if r.debug {
		go r.streamContainerLogs(ctx, containerID)
	}

	slog.Info("container started", "task", task.Id, "container", containerName)
	return nil
}

func (r *Runner) buildContainerConfig(task *xagentv1.Task, ws *workspace.Workspace) (*container.Config, *container.HostConfig, *network.NetworkingConfig) {
	ctr := &ws.Container
	taskIDStr := strconv.FormatInt(task.Id, 10)

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

func (r *Runner) copyConfig(ctx context.Context, containerID string, task *xagentv1.Task, ws *workspace.Workspace) error {
	taskIDStr := strconv.FormatInt(task.Id, 10)

	// Convert workspace to agent config format
	cfg := agent.Config{
		Cwd:        ws.Agent.Cwd,
		Prompt:     ws.Agent.Prompt,
		McpServers: make(map[string]agent.McpServer),
		Commands:   ws.Commands,
	}

	// Inject xagent MCP server for link creation
	cfg.McpServers["xagent"] = agent.McpServer{
		Type:    "stdio",
		Command: "/usr/local/bin/xagent",
		Args:    []string{"mcp", "--server", "unix:///var/run/xagent.sock", "--task", taskIDStr, "--workspace", task.Workspace},
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

func (r *Runner) streamContainerLogs(ctx context.Context, containerID string) {
	logs, err := r.docker.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		slog.Error("failed to get container logs", "container", containerID, "error", err)
		return
	}
	defer logs.Close()

	if _, err := stdcopy.StdCopy(os.Stdout, os.Stderr, logs); err != nil && err != io.EOF {
		slog.Error("failed to stream container logs", "container", containerID, "error", err)
	}
}

// Monitor watches for container exits and updates task status accordingly.
func (r *Runner) Monitor(ctx context.Context) error {
	eventCh, errCh := r.docker.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("type", "container"),
			filters.Arg("event", "die"),
			filters.Arg("label", "xagent=true"),
		),
	})

	for {
		select {
		case event := <-eventCh:
			r.runningCount.Add(-1)
			taskIDStr := event.Actor.Attributes["xagent.task"]
			taskID, err := strconv.ParseInt(taskIDStr, 10, 64)
			if err != nil {
				slog.Error("invalid task ID in container event", "task", taskIDStr, "error", err)
				continue
			}
			exitCode := event.Actor.Attributes["exitCode"]
			if exitCode == "0" {
				slog.Info("container exited successfully", "task", taskID)
				r.log(ctx, taskID, "info", "container exited successfully")
				if _, err := r.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: taskID, Status: "completed"}); err != nil {
					slog.Error("failed to update task status", "task", taskID, "error", err)
				}
				if r.notify {
					if err := notify.Send("xagent", fmt.Sprintf("%s completed", r.taskDisplayName(ctx, taskID))); err != nil {
						slog.Error("failed to send notification", "task", taskID, "error", err)
					}
				}
			} else {
				slog.Error("container exited with error", "task", taskID, "exitCode", exitCode)
				r.log(ctx, taskID, "error", fmt.Sprintf("container exited with code %s", exitCode))
				if _, err := r.client.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: taskID, Status: "failed"}); err != nil {
					slog.Error("failed to update task status", "task", taskID, "error", err)
				}
				if r.notify {
					if err := notify.Send("xagent", fmt.Sprintf("%s failed (exit code %s)", r.taskDisplayName(ctx, taskID), exitCode)); err != nil {
						slog.Error("failed to send notification", "task", taskID, "error", err)
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
		taskIDStr := c.Labels["xagent.task"]
		if taskIDStr == "" {
			continue
		}

		taskID, err := strconv.ParseInt(taskIDStr, 10, 64)
		if err != nil {
			slog.Error("invalid task ID in container label", "task", taskIDStr, "error", err)
			continue
		}

		// Fetch task status
		task, err := r.client.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
		if err != nil {
			slog.Error("failed to get task", "task", taskID, "error", err)
			continue
		}

		// Remove container if task is archived
		if task.Task.Status == "archived" {
			if err := r.docker.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
				slog.Error("failed to remove container", "task", taskID, "error", err)
			} else {
				slog.Info("container removed (task archived)", "task", taskID)
			}
		}
	}

	return nil
}
