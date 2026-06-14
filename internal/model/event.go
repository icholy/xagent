package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Event type discriminators. These are the values of the events.type column —
// a storage detail used to pick the concrete payload on read. They are not a
// field on the Event value; the type is Payload.Type().
const (
	EventTypeInstruction = "instruction"
	EventTypeExternal    = "external"
	EventTypeReport      = "report"
	EventTypeLifecycle   = "lifecycle"
	EventTypeLink        = "link"
)

// EventPayload is the sealed set of event bodies — one per arm of the
// xagentv1.Event.payload oneof. Each arm reports its discriminator via Type().
// The set is closed: implementations must live in this package because the
// interface is sealed with the unexported isEventPayload marker.
type EventPayload interface {
	// Type returns the discriminator stored in the events.type column.
	Type() string
	isEventPayload()
}

// ExternalPayload is the body of an external event — a self-contained webhook
// trigger. It is the only arm wired end to end in this increment.
type ExternalPayload struct {
	Description string `json:"description"`
	URL         string `json:"url"`
	Data        string `json:"data"`
}

func (*ExternalPayload) Type() string    { return EventTypeExternal }
func (*ExternalPayload) isEventPayload() {}

// Event is one row of a task's event stream. Its body is a typed, sealed
// payload; the events.type column is materialized from Payload.Type() purely as
// a storage discriminator and is not carried on the value.
type Event struct {
	ID        int64        `json:"id"`
	TaskID    int64        `json:"task_id"`
	OrgID     int64        `json:"org_id"`
	Wake      bool         `json:"wake"`
	Payload   EventPayload `json:"payload"`
	CreatedAt time.Time    `json:"created_at"`
}

// Proto converts an Event to its protobuf representation, mapping the typed
// payload to the matching oneof arm.
func (e *Event) Proto() *xagentv1.Event {
	pb := &xagentv1.Event{
		Id:        e.ID,
		TaskId:    e.TaskID,
		Wake:      e.Wake,
		CreatedAt: timestamppb.New(e.CreatedAt),
	}
	switch p := e.Payload.(type) {
	case *ExternalPayload:
		pb.Payload = &xagentv1.Event_External{External: &xagentv1.ExternalPayload{
			Description: p.Description,
			Url:         p.URL,
			Data:        p.Data,
		}}
	}
	return pb
}

// EventFromProto converts a protobuf Event to a model Event, mapping the set
// oneof arm to its typed payload.
func EventFromProto(pb *xagentv1.Event) *Event {
	e := &Event{
		ID:     pb.Id,
		TaskID: pb.TaskId,
		Wake:   pb.Wake,
	}
	if pb.CreatedAt != nil {
		e.CreatedAt = pb.CreatedAt.AsTime()
	}
	switch arm := pb.Payload.(type) {
	case *xagentv1.Event_External:
		e.Payload = &ExternalPayload{
			Description: arm.External.Description,
			URL:         arm.External.Url,
			Data:        arm.External.Data,
		}
	}
	return e
}
