package model

import "time"

// Log represents a log entry for a task.
type Log struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}
