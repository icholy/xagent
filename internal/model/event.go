package model

import "time"

// Event represents an external event that can trigger task actions.
type Event struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	Data        string    `json:"data"`
	URL         string    `json:"url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
