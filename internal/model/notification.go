package model

import "time"

// Notification is a lightweight change notification for one or more resources.
// Type is either "ready" (subscription is live) or "change" (resources changed).
type Notification struct {
	Type      string                 `json:"type"`
	Resources []NotificationResource `json:"resources,omitempty"`
	OrgID     int64                  `json:"org_id"`
	UserID    string                 `json:"user_id,omitempty"`
	Time      time.Time              `json:"timestamp"`
}

// NotificationResource describes an affected resource within a "change" Notification.
type NotificationResource struct {
	Action string `json:"action"` // created, updated, appended
	Type   string `json:"type"`   // task, event, log, link
	ID     int64  `json:"id"`
}
