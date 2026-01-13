package dockerx

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/container"
)

type mockWaiter struct {
	waitCh chan container.WaitResponse
	errCh  chan error
}

func (m *mockWaiter) ContainerWait(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	return m.waitCh, m.errCh
}

func TestContainerWait_Success(t *testing.T) {
	m := &mockWaiter{
		waitCh: make(chan container.WaitResponse, 1),
		errCh:  make(chan error),
	}
	m.waitCh <- container.WaitResponse{}

	err := ContainerWait(context.Background(), m, "container-id", container.WaitConditionNotRunning)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestContainerWait_Error(t *testing.T) {
	m := &mockWaiter{
		waitCh: make(chan container.WaitResponse),
		errCh:  make(chan error, 1),
	}
	m.errCh <- errors.New("some error")

	err := ContainerWait(context.Background(), m, "container-id", container.WaitConditionNotRunning)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestContainerWait_ContextCancelled(t *testing.T) {
	m := &mockWaiter{
		waitCh: make(chan container.WaitResponse),
		errCh:  make(chan error),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ContainerWait(ctx, m, "container-id", container.WaitConditionNotRunning)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}
