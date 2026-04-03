package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TaskEvent represents an event pushed to agents via channels.
type TaskEvent struct {
	ID        int64             `json:"id"`
	TaskID    int64             `json:"task_id"`
	Type      string            `json:"type"`
	Content   string            `json:"content"`
	Meta      map[string]string `json:"meta"`
	CreatedAt time.Time         `json:"created_at"`
}

// Proto converts a TaskEvent to its protobuf representation.
func (e *TaskEvent) Proto() *xagentv1.TaskEvent {
	return &xagentv1.TaskEvent{
		Id:        e.ID,
		TaskId:    e.TaskID,
		Type:      e.Type,
		Content:   e.Content,
		Meta:      e.Meta,
		CreatedAt: timestamppb.New(e.CreatedAt),
	}
}
