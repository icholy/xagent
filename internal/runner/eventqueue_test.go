package runner

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

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

	q := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})

	q.Enqueue(1, "stopped", 0)
	q.Enqueue(2, "failed", 5)
	assert.Equal(t, q.Len(), 2)

	assert.NilError(t, q.Drain(t.Context()))

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

	q := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})

	q.Enqueue(1, "started", 0)
	q.Enqueue(2, "stopped", 0)
	assert.Equal(t, q.Len(), 2)

	// First drain: first event fails, second is blocked
	assert.ErrorContains(t, q.Drain(t.Context()), "server unavailable")
	assert.Equal(t, q.Len(), 2)
	assert.Equal(t, calls, 1)

	// Second drain: first event fails again, still blocked
	assert.ErrorContains(t, q.Drain(t.Context()), "server unavailable")
	assert.Equal(t, q.Len(), 2)
	assert.Equal(t, calls, 2)

	// Third drain: first event succeeds, then second succeeds too
	assert.NilError(t, q.Drain(t.Context()))
	assert.Equal(t, q.Len(), 0)
	assert.Equal(t, calls, 4) // 2 failures + 2 successes
}

func TestEventQueue_DrainEmpty(t *testing.T) {
	mock := &xagentclient.ClientMock{}
	q := NewEventQueue(EventQueueOptions{Client: mock, Log: slog.Default()})

	assert.NilError(t, q.Drain(t.Context()))
	assert.Equal(t, q.Len(), 0)
}

func TestEventQueue_RunDrainsImmediately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var submitted []*xagentv1.SubmitRunnerEventsRequest
		mock := &xagentclient.ClientMock{
			SubmitRunnerEventsFunc: func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
				submitted = append(submitted, req)
				return &xagentv1.SubmitRunnerEventsResponse{}, nil
			},
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		q := NewEventQueue(EventQueueOptions{
			Client:        mock,
			Log:           slog.Default(),
			RetryInterval: time.Minute,
		})
		go q.Run(ctx)

		q.Enqueue(1, "started", 0)
		synctest.Wait()

		assert.Equal(t, q.Len(), 0)
		assert.Equal(t, len(submitted), 1)
		assert.Equal(t, submitted[0].Events[0].TaskId, int64(1))
	})
}

func TestEventQueue_RunRetriesAfterInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		mock := &xagentclient.ClientMock{
			SubmitRunnerEventsFunc: func(_ context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
				calls++
				if calls <= 1 {
					return nil, fmt.Errorf("server unavailable")
				}
				return &xagentv1.SubmitRunnerEventsResponse{}, nil
			},
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		q := NewEventQueue(EventQueueOptions{
			Client:        mock,
			Log:           slog.Default(),
			RetryInterval: 5 * time.Second,
		})
		go q.Run(ctx)

		q.Enqueue(1, "started", 0)
		synctest.Wait()

		// First attempt failed, event still queued
		assert.Equal(t, q.Len(), 1)
		assert.Equal(t, calls, 1)

		// After retry interval, it retries and succeeds
		time.Sleep(5 * time.Second)
		synctest.Wait()

		assert.Equal(t, q.Len(), 0)
		assert.Equal(t, calls, 2)
	})
}
