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

func TestContainerKill_Success(t *testing.T) {
	waitCh := make(chan container.WaitResponse, 1)
	errCh := make(chan error)
	waitCh <- container.WaitResponse{}

	m := &ContainerKillerMock{
		ContainerKillFunc: func(ctx context.Context, containerID string, signal string) error {
			return nil
		},
		ContainerWaitFunc: func(ctx context.Context, containerID string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
			return waitCh, errCh
		},
	}

	err := ContainerKill(t.Context(), m, "container-id", "SIGTERM")
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	killCalls := m.ContainerKillCalls()
	if len(killCalls) != 1 {
		t.Errorf("expected 1 kill call, got %d", len(killCalls))
	}
	if killCalls[0].Signal != "SIGTERM" {
		t.Errorf("expected signal SIGTERM, got %s", killCalls[0].Signal)
	}
}

type conflictError struct {
	msg string
}

func (e *conflictError) Error() string { return e.msg }
func (e *conflictError) Conflict()     {}

func TestContainerKill_NotRunning(t *testing.T) {
	m := &ContainerKillerMock{
		ContainerKillFunc: func(ctx context.Context, containerID string, signal string) error {
			return &conflictError{msg: "container abc123 is not running"}
		},
	}

	err := ContainerKill(t.Context(), m, "container-id", "SIGTERM")
	if !errors.Is(err, ErrNotRunning) {
		t.Errorf("expected ErrNotRunning, got: %v", err)
	}
}

func TestContainerKill_OtherError(t *testing.T) {
	m := &ContainerKillerMock{
		ContainerKillFunc: func(ctx context.Context, containerID string, signal string) error {
			return errors.New("some other error")
		},
	}

	err := ContainerKill(t.Context(), m, "container-id", "SIGTERM")
	if err == nil {
		t.Error("expected error, got nil")
	}
	if errors.Is(err, ErrNotRunning) {
		t.Error("expected non-ErrNotRunning error")
	}
}

