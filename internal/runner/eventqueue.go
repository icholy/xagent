package runner

import (
	"container/list"
	"context"
	"log/slog"
	"sync"
	"time"

	"connectrpc.com/connect"

	"github.com/icholy/xagent/internal/common"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
)

// EventQueue buffers SubmitRunnerEvents calls that fail and retries them
// on the next Drain call. Events are sent in FIFO order; if any event
// fails, all subsequent events are blocked until it succeeds.
type EventQueue struct {
	mu            sync.Mutex
	events        *list.List
	notify        chan struct{}
	client        xagentclient.Client
	log           *slog.Logger
	retryInterval time.Duration
}

// EventQueueOptions configures the EventQueue.
type EventQueueOptions struct {
	Client        xagentclient.Client
	Log           *slog.Logger
	RetryInterval time.Duration
}

// NewEventQueue creates a new in-memory event queue.
func NewEventQueue(opts EventQueueOptions) *EventQueue {
	return &EventQueue{
		events:        list.New(),
		notify:        make(chan struct{}, 1),
		client:        opts.Client,
		log:           opts.Log,
		retryInterval: opts.RetryInterval,
	}
}

// Enqueue adds an event to the queue.
func (q *EventQueue) Enqueue(event *xagentv1.RunnerEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events.PushBack(event)
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// Len returns the number of events in the queue.
func (q *EventQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.events.Len()
}

// Drain sends queued events in FIFO order. On failure it returns the
// error and leaves remaining events in the queue for the next call.
func (q *EventQueue) Drain(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.events.Len() > 0 {
		el := q.events.Front()
		ev := el.Value.(*xagentv1.RunnerEvent)
		_, err := q.client.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
			Events: []*xagentv1.RunnerEvent{ev},
		})
		if err != nil {
			if isPermanentError(err) {
				q.log.Warn("event dropped due to permanent error", "task", ev.TaskId, "event", ev.Event, "error", err)
				q.events.Remove(el)
				continue
			}
			return err
		}
		q.log.Info("event delivered", "task", ev.TaskId, "event", ev.Event)
		q.events.Remove(el)
	}
	return nil
}

// isPermanentError returns true if the error indicates a condition that
// will never succeed on retry (e.g. task not found, invalid argument).
func isPermanentError(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodeNotFound, connect.CodeInvalidArgument, connect.CodePermissionDenied:
		return true
	default:
		return false
	}
}

// Run drains the queue whenever events are enqueued. On error it waits
// for the configured retry interval before trying again.
func (q *EventQueue) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.notify:
		}
		for i := 1; true; i++ {
			err := q.Drain(ctx)
			if err == nil {
				break
			}
			q.log.Warn("event delivery failed, will retry", "error", err, "queued", q.Len(), "failures", i)
			if !common.SleepContext(ctx, q.retryInterval) {
				return
			}
		}
	}
}
