package dockerx

//go:generate go tool moq -pkg dockerx -out container_waiter_moq_test.go . ContainerWaiter
//go:generate go tool moq -pkg dockerx -out container_killer_moq_test.go . ContainerKiller

import (
	"context"
	"errors"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
)

// ErrNotRunning is returned when attempting to kill a container that is not running.
var ErrNotRunning = errors.New("container not running")

// ContainerWaiter is a minimal interface for waiting on containers.
type ContainerWaiter interface {
	ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error)
}

// ContainerKiller is a minimal interface for killing and waiting on containers.
type ContainerKiller interface {
	ContainerKill(ctx context.Context, containerID string, signal string) error
	ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error)
}

// ContainerWait waits for a container to reach the specified condition.
// It blocks until the container reaches the condition, an error occurs, or the context is cancelled.
func ContainerWait(ctx context.Context, client ContainerWaiter, containerID string, condition container.WaitCondition) error {
	waitCh, errCh := client.ContainerWait(ctx, containerID, condition)
	select {
	case <-waitCh:
		return nil
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ContainerKill kills a container with the specified signal and waits for it to stop.
// It returns ErrNotRunning if the container was not running.
func ContainerKill(ctx context.Context, client ContainerKiller, containerID string, signal string) error {
	if err := client.ContainerKill(ctx, containerID, signal); err != nil {
		if cerrdefs.IsConflict(err) && strings.Contains(err.Error(), "is not running") {
			return ErrNotRunning
		}
		return err
	}
	return ContainerWait(ctx, client, containerID, container.WaitConditionNotRunning)
}
