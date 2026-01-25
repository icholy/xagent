package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Event represents an external event that can trigger task actions.
type Event struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	Data        string    `json:"data"`
	URL         string    `json:"url,omitempty"`
	Owner       string    `json:"owner"`
	CreatedAt   time.Time `json:"created_at"`
}

// Proto converts an Event to its protobuf representation.
func (e *Event) Proto() *xagentv1.Event {
	return &xagentv1.Event{
		Id:          e.ID,
		Description: e.Description,
		Data:        e.Data,
		Url:         e.URL,
		CreatedAt:   timestamppb.New(e.CreatedAt),
	}
}

// EventFromProto converts a protobuf Event to a model Event.
func EventFromProto(pb *xagentv1.Event) *Event {
	var createdAt time.Time
	if pb.CreatedAt != nil {
		createdAt = pb.CreatedAt.AsTime()
	}
	return &Event{
		ID:          pb.Id,
		Description: pb.Description,
		Data:        pb.Data,
		URL:         pb.Url,
		CreatedAt:   createdAt,
	}
}
