package model

import "time"

// Notification is a lightweight change notification for one or more resources.
// Type is either "ready" (subscription is live) or "change" (resources changed).
type Notification struct {
	Type      string                 `json:"type"`
	Resources []NotificationResource `json:"resources,omitempty"`
	Time      time.Time              `json:"timestamp"`
	OrgID     int64                  `json:"org_id"`
	UserID    string                 `json:"user_id,omitempty"`
	ClientID  string                 `json:"client_id,omitempty"`
	// Runner is only set if there's pending work to do
	Runner string `json:"for_runner,omitempty"`
	// TaskEvent is the causal task event behind this notification — the
	// external event when the change was event-driven, else the lifecycle
	// event for the transition. Consumers derive their own semantics from it:
	// `xagent notify` surfaces terminal lifecycle outcomes and the mcp channel
	// bridge renders a forwarded line. Nil for notifications with no
	// channel-relevant event (log uploads, link creation). Carried over the
	// JSON wire via model.Event's discriminator-inlining codec.
	TaskEvent *Event `json:"event,omitempty"`
	// Ignore causes the publisher to skip this notification entirely. Lets
	// callers always build and "publish" a notification and decide inside
	// the transaction whether it actually goes out, instead of gating the
	// publish call with a separate boolean.
	Ignore bool `json:"-"`
}

// NotificationResource describes an affected resource within a "change" Notification.
type NotificationResource struct {
	Action string `json:"action"` // created, updated, appended
	Type   string `json:"type"`   // task, event, log, link
	ID     int64  `json:"id"`
}
