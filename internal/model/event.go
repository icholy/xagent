package model

import (
	"encoding/json"
	"fmt"
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Event types are materialized from the set arm of the Event.payload oneof and
// stored in the `type` column for indexed filtering. The set arm IS the type —
// there is no separate type field on the wire.
const (
	EventTypeInstruction = "instruction"
	EventTypeExternal    = "external"
	EventTypeReport      = "report"
	EventTypeLifecycle   = "lifecycle"
	EventTypeLink        = "link"
)

// Event is one row of a task's event stream. Its body is a typed payload — the
// set arm of xagentv1.Event.payload — persisted as a `type` string plus the
// arm's protojson encoding in the `payload` jsonb column.
type Event struct {
	ID        int64           `json:"id"`
	TaskID    int64           `json:"task_id"`
	OrgID     int64           `json:"org_id"`
	Type      string          `json:"type"`
	Wake      bool            `json:"wake"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// NewExternalEvent builds an external event whose payload is the protojson
// encoding of the given ExternalPayload. Only the external arm is wired in this
// increment; the other arms are defined on the wire but not yet stored.
func NewExternalEvent(taskID, orgID int64, wake bool, payload *xagentv1.ExternalPayload) (*Event, error) {
	raw, err := protojson.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal external payload: %w", err)
	}
	return &Event{
		TaskID:  taskID,
		OrgID:   orgID,
		Type:    EventTypeExternal,
		Wake:    wake,
		Payload: raw,
	}, nil
}

// Proto converts an Event to its protobuf representation, decoding the payload
// jsonb back into the matching oneof arm.
func (e *Event) Proto() *xagentv1.Event {
	pb := &xagentv1.Event{
		Id:        e.ID,
		TaskId:    e.TaskID,
		Wake:      e.Wake,
		CreatedAt: timestamppb.New(e.CreatedAt),
	}
	setEventPayload(pb, e.Type, e.Payload)
	return pb
}

// EventFromProto converts a protobuf Event to a model Event, materializing the
// set oneof arm into the type string and the arm's protojson payload.
func EventFromProto(pb *xagentv1.Event) *Event {
	e := &Event{
		ID:     pb.Id,
		TaskID: pb.TaskId,
		Wake:   pb.Wake,
	}
	if pb.CreatedAt != nil {
		e.CreatedAt = pb.CreatedAt.AsTime()
	}
	if typ, raw := eventPayload(pb); raw != nil {
		e.Type = typ
		e.Payload = raw
	}
	return e
}

// setEventPayload decodes the protojson payload into the oneof arm named by typ
// and assigns it to pb. A payload that fails to decode leaves pb's payload unset
// rather than failing the whole conversion.
func setEventPayload(pb *xagentv1.Event, typ string, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	switch typ {
	case EventTypeInstruction:
		var p xagentv1.InstructionPayload
		if protojson.Unmarshal(raw, &p) == nil {
			pb.Payload = &xagentv1.Event_Instruction{Instruction: &p}
		}
	case EventTypeExternal:
		var p xagentv1.ExternalPayload
		if protojson.Unmarshal(raw, &p) == nil {
			pb.Payload = &xagentv1.Event_External{External: &p}
		}
	case EventTypeReport:
		var p xagentv1.ReportPayload
		if protojson.Unmarshal(raw, &p) == nil {
			pb.Payload = &xagentv1.Event_Report{Report: &p}
		}
	case EventTypeLifecycle:
		var p xagentv1.LifecyclePayload
		if protojson.Unmarshal(raw, &p) == nil {
			pb.Payload = &xagentv1.Event_Lifecycle{Lifecycle: &p}
		}
	case EventTypeLink:
		var p xagentv1.LinkPayload
		if protojson.Unmarshal(raw, &p) == nil {
			pb.Payload = &xagentv1.Event_Link{Link: &p}
		}
	}
}

// eventPayload returns the type string and protojson encoding of the set oneof
// arm of pb. It returns ("", nil) when no arm is set.
func eventPayload(pb *xagentv1.Event) (string, json.RawMessage) {
	switch arm := pb.Payload.(type) {
	case *xagentv1.Event_Instruction:
		return EventTypeInstruction, mustMarshalPayload(arm.Instruction)
	case *xagentv1.Event_External:
		return EventTypeExternal, mustMarshalPayload(arm.External)
	case *xagentv1.Event_Report:
		return EventTypeReport, mustMarshalPayload(arm.Report)
	case *xagentv1.Event_Lifecycle:
		return EventTypeLifecycle, mustMarshalPayload(arm.Lifecycle)
	case *xagentv1.Event_Link:
		return EventTypeLink, mustMarshalPayload(arm.Link)
	default:
		return "", nil
	}
}

// mustMarshalPayload protojson-encodes a payload arm. Encoding a valid proto
// message never fails, so a marshal error degrades to a null payload.
func mustMarshalPayload(m proto.Message) json.RawMessage {
	raw, err := protojson.Marshal(m)
	if err != nil {
		return nil
	}
	return raw
}
