package dockerx

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestContainerWait_Success(t *testing.T) {
	waitCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error)
	waitCh <- container.WaitResponse{}

	m := &ContainerWaiterMock{
		ContainerWaitFunc: func(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
			return waitCh, errCh
		},
	}

	err := ContainerWait(t.Context(), m, "container-id", container.WaitConditionNotRunning)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestContainerWait_Error(t *testing.T) {
	waitCh := make(chan container.WaitResponse)
	errCh := make(chan error, 1)
	errCh <- errors.New("some error")

	m := &ContainerWaiterMock{
		ContainerWaitFunc: func(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
			return waitCh, errCh
		},
	}

	err := ContainerWait(t.Context(), m, "container-id", container.WaitConditionNotRunning)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestContainerWait_ContextCancelled(t *testing.T) {
	waitCh := make(chan container.WaitResponse)
	errCh := make(chan error)

	m := &ContainerWaiterMock{
		ContainerWaitFunc: func(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
			return waitCh, errCh
		},
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := ContainerWait(ctx, m, "container-id", container.WaitConditionNotRunning)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}
