package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Link represents a link between a task and an external resource.
type Link struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Relevance string    `json:"relevance"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Notify    bool      `json:"notify"`
	CreatedAt time.Time `json:"created_at"`
}

// Proto converts a Link to its protobuf representation.
func (l *Link) Proto() *xagentv1.TaskLink {
	return &xagentv1.TaskLink{
		Id:        l.ID,
		TaskId:    l.TaskID,
		Relevance: l.Relevance,
		Url:       l.URL,
		Title:     l.Title,
		Notify:    l.Notify,
		CreatedAt: timestamppb.New(l.CreatedAt),
	}
}

// LinkFromProto converts a protobuf TaskLink to a model Link.
func LinkFromProto(pb *xagentv1.TaskLink) *Link {
	var createdAt time.Time
	if pb.CreatedAt != nil {
		createdAt = pb.CreatedAt.AsTime()
	}
	return &Link{
		ID:        pb.Id,
		TaskID:    pb.TaskId,
		Relevance: pb.Relevance,
		URL:       pb.Url,
		Title:     pb.Title,
		Notify:    pb.Notify,
		CreatedAt: createdAt,
	}
}
