package model

import "time"

// Notification is a lightweight change notification for a resource.
type Notification struct {
	Type     string    `json:"type"`
	Resource string    `json:"resource"`
	ID       int64     `json:"id"`
	OrgID    int64     `json:"org_id"`
	Version  int64     `json:"version"`
	Time     time.Time `json:"timestamp"`
}
