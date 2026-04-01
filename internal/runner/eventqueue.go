package runner

import (
	"container/list"
	"context"
	"log/slog"
	"sync"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
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
	mu     sync.Mutex
	events *list.List
	client xagentclient.Client
	log    *slog.Logger
}

// NewEventQueue creates a new in-memory event queue.
func NewEventQueue(client xagentclient.Client, log *slog.Logger) *EventQueue {
	return &EventQueue{
		events: list.New(),
		client: client,
		log:    log,
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
}

// Len returns the number of events in the queue.
func (q *EventQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.events.Len()
}

// Drain sends queued events in FIFO order. On failure, all remaining
// events are blocked until the next Drain call.
func (q *EventQueue) Drain(ctx context.Context) {
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
			q.log.Warn("event delivery failed, will retry", "task", ev.TaskID, "event", ev.Event, "error", err)
			return
		}
		q.log.Info("event delivered", "task", ev.TaskID, "event", ev.Event)
		q.events.Remove(el)
	}
}
