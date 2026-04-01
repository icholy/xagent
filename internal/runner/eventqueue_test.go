package runner

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"gotest.tools/v3/assert"
)

func TestEventQueue_DrainSuccess(t *testing.T) {
	var submitted []*xagentv1.SubmitRunnerEventsRequest
	mock := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			submitted = append(submitted, req)
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}

	q := NewEventQueue(mock, slog.Default())

	q.Enqueue(1, "stopped", 0)
	q.Enqueue(2, "failed", 5)
	assert.Equal(t, q.Len(), 2)

	q.Drain(t.Context())

	assert.Equal(t, q.Len(), 0)
	assert.Equal(t, len(submitted), 2)
	assert.Equal(t, submitted[0].Events[0].TaskId, int64(1))
	assert.Equal(t, submitted[0].Events[0].Event, "stopped")
	assert.Equal(t, submitted[1].Events[0].TaskId, int64(2))
	assert.Equal(t, submitted[1].Events[0].Event, "failed")
}

func TestEventQueue_DrainBlocksOnFailure(t *testing.T) {
	calls := 0
	mock := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			calls++
			if calls <= 2 {
				return nil, fmt.Errorf("server unavailable")
			}
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}

	q := NewEventQueue(mock, slog.Default())

	q.Enqueue(1, "started", 0)
	q.Enqueue(2, "stopped", 0)
	assert.Equal(t, q.Len(), 2)

	// First drain: first event fails, second is blocked
	q.Drain(t.Context())
	assert.Equal(t, q.Len(), 2)
	assert.Equal(t, calls, 1)

	// Second drain: first event fails again, still blocked
	q.Drain(t.Context())
	assert.Equal(t, q.Len(), 2)
	assert.Equal(t, calls, 2)

	// Third drain: first event succeeds, then second succeeds too
	q.Drain(t.Context())
	assert.Equal(t, q.Len(), 0)
	assert.Equal(t, calls, 4) // 2 failures + 2 successes
}

func TestEventQueue_DrainEmpty(t *testing.T) {
	mock := &xagentclient.ClientMock{}
	q := NewEventQueue(mock, slog.Default())

	q.Drain(t.Context())
	assert.Equal(t, q.Len(), 0)
}
