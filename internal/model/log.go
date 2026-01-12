package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

// Log represents a log entry for a task.
type Log struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Proto converts a Log to its protobuf LogEntry representation.
func (l *Log) Proto() *xagentv1.LogEntry {
	return &xagentv1.LogEntry{
		Type:    l.Type,
		Content: l.Content,
	}
}
