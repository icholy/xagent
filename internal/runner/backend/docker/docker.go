// Package docker implements the runner backend on the local Docker daemon.
// Containers are named xagent-{task-id} and labelled with the owning runner
// so multiple runners can share a daemon.
package docker

import (
	"archive/tar"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/icholy/xagent/internal/runner/backend"
	"github.com/icholy/xagent/internal/runner/prebuilt"
	"github.com/icholy/xagent/internal/x/dockerx"
)

// Backend runs task sandboxes as containers on the local Docker daemon.
type Backend struct {
	docker   *client.Client
	runnerID string
	log      *slog.Logger
}

type Options struct {
	RunnerID string
	Log      *slog.Logger
}

func New(opts Options) (*Backend, error) {
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	return &Backend{
		docker:   docker,
		runnerID: opts.RunnerID,
		log:      cmp.Or(opts.Log, slog.Default()),
	}, nil
}

func (b *Backend) Close() error {
	return b.docker.Close()
}

// find returns the container for the given task.
// Returns (container, true, nil) if found, (empty, false, nil) if not found,
// or (empty, false, error) on error.
func (b *Backend) find(ctx context.Context, taskID int64) (container.Summary, bool, error) {
	containers, err := b.docker.ContainerList(ctx, container.ListOptions{
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

// Start finds or creates the task's container and starts it. A previous
// container for the same task is reused, so its filesystem persists across
// restarts.
func (b *Backend) Start(ctx context.Context, spec *backend.Spec) error {
	c, ok, err := b.find(ctx, spec.TaskID)
	if err != nil {
		return err
	}

	var containerID string
	if ok {
		b.log.Info("starting existing container", "task", spec.TaskID, "name", fmt.Sprintf("xagent-%d", spec.TaskID))
		containerID = c.ID

		// Refresh any network attachments whose endpoint ID has drifted
		// from the live network — e.g. after `docker compose down && up`.
		repaired, err := dockerx.RepairNetworks(ctx, b.docker, containerID, spec.Workspace.Container.Networks)
		if err != nil {
			return fmt.Errorf("failed to repair network attachment: %w", err)
		}
		if len(repaired) > 0 {
			b.log.Warn("repaired stale network attachments",
				"task", spec.TaskID, "container", containerID, "networks", repaired)
		}
	} else {
		containerID, err = b.create(ctx, spec)
		if err != nil {
			return err
		}
	}

	if err := b.docker.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	return nil
}

func (b *Backend) create(ctx context.Context, spec *backend.Spec) (string, error) {
	wc := &spec.Workspace.Container

	b.log.Info("creating container", "task", spec.TaskID, "image", wc.Image)

	// Ensure the image is available locally (pulls if needed).
	info, err := dockerx.ImageEnsure(ctx, b.docker, dockerx.ImageEnsureOptions{
		Ref:         wc.Image,
		PullOptions: image.PullOptions{RegistryAuth: dockerx.ResolveRegistryAuth(wc.Image)},
		PullProgress: func(p dockerx.PullProgress) {
			if p.Status != "" && p.Progress == "" {
				b.log.Info("pull", "image", wc.Image, "status", p.Status)
			}
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to ensure image: %w", err)
	}

	// The driver binary is provisioned by the backend because selecting it
	// requires the image architecture, which only the runtime knows.
	binData, err := prebuilt.ReadBinary(info.Architecture)
	if err != nil {
		return "", err
	}

	resp, err := b.docker.ContainerCreate(ctx,
		&container.Config{
			Image: wc.Image,
			User:  wc.User,
			Labels: map[string]string{
				"xagent":        "true",
				"xagent.task":   fmt.Sprint(spec.TaskID),
				"xagent.runner": b.runnerID,
			},
			Cmd:        spec.Cmd,
			Env:        append(wc.Environ(), spec.Env...),
			WorkingDir: wc.WorkingDir,
		},
		&container.HostConfig{
			Binds:       wc.Volumes,
			GroupAdd:    wc.GroupAdd,
			Runtime:     wc.Runtime,
			Privileged:  wc.Privileged,
			NetworkMode: container.NetworkMode(wc.NetworkMode),
		},
		wc.NetworkingConfig(),
		nil,
		fmt.Sprintf("xagent-%d", spec.TaskID),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	files := append([]backend.File{
		{Path: backend.BinaryPath, Data: binData, Mode: 0755},
	}, spec.Files...)
	if err := b.copyFiles(ctx, resp.ID, files); err != nil {
		return "", fmt.Errorf("failed to copy files: %w", err)
	}

	return resp.ID, nil
}

func (b *Backend) copyFiles(ctx context.Context, containerID string, files []backend.File) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range files {
		name := strings.TrimPrefix(f.Path, "/")
		if f.Dir {
			if err := tw.WriteHeader(&tar.Header{
				Name:     name + "/",
				Mode:     f.Mode,
				Typeflag: tar.TypeDir,
			}); err != nil {
				return err
			}
			continue
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: f.Mode,
			Size: int64(len(f.Data)),
		}); err != nil {
			return err
		}
		if _, err := tw.Write(f.Data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return b.docker.CopyToContainer(ctx, containerID, "/", &buf, container.CopyToContainerOptions{})
}

// Stop stops the task's container if it is running: SIGTERM first, then
// SIGKILL after a 30s grace period. It reports whether a running container
// was signalled — in that case the driver owns the terminal event report
// (see the driver-owned-events proposal).
func (b *Backend) Stop(ctx context.Context, taskID int64) (bool, error) {
	c, ok, err := b.find(ctx, taskID)
	if err != nil {
		return false, err
	}
	if !ok || c.State != "running" {
		return false, nil
	}
	b.log.Info("killing container", "task", taskID)

	// Try SIGTERM first with a timeout
	killCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = dockerx.ContainerKill(killCtx, b.docker, c.ID, "SIGTERM")
	if err == nil {
		return true, nil
	}
	if errors.Is(err, dockerx.ErrNotRunning) {
		// The container exited between the running check and the kill, so
		// nothing was signalled.
		return false, nil
	}

	// If SIGTERM timed out, send SIGKILL
	if errors.Is(err, context.DeadlineExceeded) {
		b.log.Warn("SIGTERM timed out, sending SIGKILL", "task", taskID)
		if err := dockerx.ContainerKill(ctx, b.docker, c.ID, "SIGKILL"); err != nil && !errors.Is(err, dockerx.ErrNotRunning) {
			return true, err
		}
		return true, nil
	}

	return true, err
}

func (b *Backend) Running(ctx context.Context, taskID int64) (bool, error) {
	c, ok, err := b.find(ctx, taskID)
	if err != nil || !ok {
		return false, err
	}
	return c.State == "running", nil
}

func (b *Backend) List(ctx context.Context) ([]backend.Sandbox, error) {
	containers, err := b.docker.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", "xagent=true"),
			filters.Arg("label", "xagent.runner="+b.runnerID),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	var sandboxes []backend.Sandbox
	for _, c := range containers {
		label := c.Labels["xagent.task"]
		if label == "" {
			continue
		}
		taskID, err := strconv.ParseInt(label, 10, 64)
		if err != nil {
			b.log.Error("invalid task ID in container label", "xagent.task", label, "error", err)
			continue
		}
		sandboxes = append(sandboxes, backend.Sandbox{TaskID: taskID, State: sandboxState(c.State)})
	}
	return sandboxes, nil
}

func sandboxState(state string) backend.State {
	switch state {
	case "running":
		return backend.StateRunning
	case "exited", "dead":
		return backend.StateExited
	default:
		return backend.StateUnknown
	}
}

func (b *Backend) Remove(ctx context.Context, taskID int64) error {
	c, ok, err := b.find(ctx, taskID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return b.docker.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
}

// Watch subscribes to docker die events for this runner's containers and
// invokes handle once per exit. An unparseable exit code is reported as
// non-zero: by the driver-owned-events invariant that means "report lost",
// and the status guard rejects the resulting failed event if the driver
// already reported.
func (b *Backend) Watch(ctx context.Context, handle func(backend.Exit)) error {
	eventCh, errCh := b.docker.Events(ctx, events.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("type", "container"),
			filters.Arg("event", "die"),
			filters.Arg("label", "xagent=true"),
			filters.Arg("label", "xagent.runner="+b.runnerID),
		),
	})

	for {
		select {
		case event := <-eventCh:
			taskIDStr := event.Actor.Attributes["xagent.task"]
			taskID, err := strconv.ParseInt(taskIDStr, 10, 64)
			if err != nil {
				b.log.Error("invalid task ID in container event", "task", taskIDStr, "error", err)
				continue
			}
			exitCode, err := strconv.Atoi(event.Actor.Attributes["exitCode"])
			if err != nil {
				b.log.Error("invalid exit code in container event", "task", taskID, "exitCode", event.Actor.Attributes["exitCode"], "error", err)
				exitCode = -1
			}
			handle(backend.Exit{TaskID: taskID, ExitCode: exitCode})

		case err := <-errCh:
			return fmt.Errorf("docker events error: %w", err)

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
