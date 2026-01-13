package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
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
		Type:      l.Type,
		Content:   l.Content,
		CreatedAt: timestamppb.New(l.CreatedAt),
	}
}

// LogFromProto converts a protobuf LogEntry to a model Log.
// Note: ID, TaskID, and CreatedAt must be set separately as they are not
// part of the LogEntry protobuf message.
func LogFromProto(pb *xagentv1.LogEntry) Log {
	return Log{
		Type:    pb.Type,
		Content: pb.Content,
	}
}
