package dockerx

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
)

// ContainerWaiter is a minimal interface for waiting on containers.
type ContainerWaiter interface {
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
		return fmt.Errorf("failed to wait for container: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}
}
