package pubsub

import (
	"context"
	"log/slog"
	"sync"
)

const subscriberBufSize = 64

// LocalPubSub is an in-process pub/sub implementation keyed by org ID.
type LocalPubSub struct {
	mu   sync.RWMutex
	subs map[int64][]chan Notification
}

// NewLocalPubSub returns a new LocalPubSub.
func NewLocalPubSub() *LocalPubSub {
	return &LocalPubSub{
		subs: make(map[int64][]chan Notification),
	}
}

// Publish fans out the notification to all current subscribers for the org.
// A slow subscriber whose buffer is full will have the notification dropped
// rather than blocking the publisher.
func (ps *LocalPubSub) Publish(ctx context.Context, orgID int64, n Notification) error {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	for _, ch := range ps.subs[orgID] {
		select {
		case ch <- n:
		default:
			slog.Warn("dropping notification for slow subscriber",
				"org_id", orgID,
				"resource", n.Resource,
				"type", n.Type,
				"id", n.ID,
			)
		}
	}
	return nil
}

// Subscribe registers a new subscriber for the given org. It returns a channel
// that receives notifications and a cancel func that removes the subscription
// and closes the channel. The cancel func is safe to call multiple times.
// If the context is cancelled, the subscription is also removed.
func (ps *LocalPubSub) Subscribe(ctx context.Context, orgID int64) (<-chan Notification, func(), error) {
	ch := make(chan Notification, subscriberBufSize)

	ps.mu.Lock()
	ps.subs[orgID] = append(ps.subs[orgID], ch)
	ps.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			ps.mu.Lock()
			defer ps.mu.Unlock()
			subs := ps.subs[orgID]
			for i, s := range subs {
				if s == ch {
					ps.subs[orgID] = append(subs[:i], subs[i+1:]...)
					break
				}
			}
			close(ch)
		})
	}

	go func() {
		select {
		case <-ctx.Done():
			cancel()
		}
	}()

	return ch, cancel, nil
}
