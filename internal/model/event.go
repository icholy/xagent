package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/proto"
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
// xagentv1.Event.payload oneof. Each arm reports its discriminator via Type()
// and maps itself onto the wire via Proto()/SetPayload, keeping the arm switch
// off Event.Proto(). The set is closed: implementations must live in this
// package because the interface is sealed with the unexported isEventPayload
// marker.
type EventPayload interface {
	// Type returns the discriminator stored in the events.type column.
	Type() string
	// Proto returns the inner oneof arm message (e.g. *xagentv1.ExternalPayload).
	Proto() proto.Message
	// SetPayload assigns this arm onto pb.Payload. It does the assignment rather
	// than returning the arm because protoc-gen-go's oneof wrapper type is
	// unexported and unnameable from this package.
	SetPayload(pb *xagentv1.Event)
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

func (p *ExternalPayload) Proto() proto.Message {
	return &xagentv1.ExternalPayload{Description: p.Description, Url: p.URL, Data: p.Data}
}

func (p *ExternalPayload) SetPayload(pb *xagentv1.Event) {
	pb.Payload = &xagentv1.Event_External{External: &xagentv1.ExternalPayload{
		Description: p.Description,
		Url:         p.URL,
		Data:        p.Data,
	}}
}

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

// Proto converts an Event to its protobuf representation, delegating the arm
// mapping to the payload.
func (e *Event) Proto() *xagentv1.Event {
	pb := &xagentv1.Event{
		Id:        e.ID,
		TaskId:    e.TaskID,
		Wake:      e.Wake,
		CreatedAt: timestamppb.New(e.CreatedAt),
	}
	e.Payload.SetPayload(pb)
	return pb
}

// EventFromProto converts a protobuf Event to a model Event, delegating the arm
// mapping to EventPayloadFromProto.
func EventFromProto(pb *xagentv1.Event) *Event {
	e := &Event{
		ID:      pb.Id,
		TaskID:  pb.TaskId,
		Wake:    pb.Wake,
		Payload: EventPayloadFromProto(pb),
	}
	if pb.CreatedAt != nil {
		e.CreatedAt = pb.CreatedAt.AsTime()
	}
	return e
}

// EventPayloadFromProto maps the set oneof arm of pb to its typed payload. It
// is the only proto→model arm switch; it returns nil when no arm is set.
func EventPayloadFromProto(pb *xagentv1.Event) EventPayload {
	switch arm := pb.Payload.(type) {
	case *xagentv1.Event_External:
		return &ExternalPayload{
			Description: arm.External.Description,
			URL:         arm.External.Url,
			Data:        arm.External.Data,
		}
	default:
		return nil
	}
}
