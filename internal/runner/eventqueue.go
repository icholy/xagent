package runner

import (
	"container/list"
	"context"
	"log/slog"
	"sync"
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/common"
	"github.com/icholy/xagent/internal/xagentclient"
)

// queuedEvent is a runner event waiting to be retried.
type queuedEvent struct {
	TaskID  int64
	Event   string
	Version int64
}

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
func (q *EventQueue) Enqueue(taskID int64, event string, version int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events.PushBack(queuedEvent{
		TaskID:  taskID,
		Event:   event,
		Version: version,
	})
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
		ev := el.Value.(queuedEvent)
		_, err := q.client.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
			Events: []*xagentv1.RunnerEvent{
				{TaskId: ev.TaskID, Event: ev.Event, Version: ev.Version},
			},
		})
		if err != nil {
			return err
		}
		q.log.Info("event delivered", "task", ev.TaskID, "event", ev.Event)
		q.events.Remove(el)
	}
	return nil
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
		for {
			err := q.Drain(ctx)
			if err == nil {
				break
			}
			q.log.Warn("event delivery failed, will retry", "error", err)
			if !common.SleepContext(ctx, q.retryInterval) {
				return
			}
		}
	}
}
