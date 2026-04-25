package pubsub

import (
	"context"
	"time"
)

// Notification is a lightweight change notification for a resource.
type Notification struct {
	Type     string    `json:"type"`
	Resource string    `json:"resource"`
	ID       int64     `json:"id"`
	OrgID    int64     `json:"org_id"`
	Version  int64     `json:"version"`
	Time     time.Time `json:"timestamp"`
}

// Publisher publishes notifications to subscribers of the given org.
type Publisher interface {
	Publish(ctx context.Context, orgID int64, n Notification) error
}

// Subscriber subscribes to notifications for the given org. The returned
// channel receives notifications until the cancel func is called or the
// context is cancelled. The channel is closed when the subscription ends.
// Calling the cancel func more than once is safe.
type Subscriber interface {
	Subscribe(ctx context.Context, orgID int64) (<-chan Notification, func(), error)
}
