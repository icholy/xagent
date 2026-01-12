package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Proto converts an Instruction to its protobuf representation.
func (i *Instruction) Proto() *xagentv1.Instruction {
	return &xagentv1.Instruction{
		Text: i.Text,
		Url:  i.URL,
	}
}

// InstructionFromProto converts a protobuf Instruction to a model Instruction.
func InstructionFromProto(pb *xagentv1.Instruction) Instruction {
	return Instruction{
		Text: pb.Text,
		URL:  pb.Url,
	}
}

// Proto converts a Task to its protobuf representation.
func (t *Task) Proto() *xagentv1.Task {
	instructions := make([]*xagentv1.Instruction, len(t.Instructions))
	for i, inst := range t.Instructions {
		instructions[i] = inst.Proto()
	}
	return &xagentv1.Task{
		Id:           t.ID,
		Name:         t.Name,
		Parent:       t.Parent,
		Workspace:    t.Workspace,
		Instructions: instructions,
		Status:       string(t.Status),
		Command:      string(t.Command),
		Version:      t.Version,
		CreatedAt:    timestamppb.New(t.CreatedAt),
		UpdatedAt:    timestamppb.New(t.UpdatedAt),
	}
}

// TaskFromProto converts a protobuf Task to a model Task.
func TaskFromProto(pb *xagentv1.Task) *Task {
	instructions := make([]Instruction, len(pb.Instructions))
	for i, inst := range pb.Instructions {
		instructions[i] = InstructionFromProto(inst)
	}
	var createdAt, updatedAt time.Time
	if pb.CreatedAt != nil {
		createdAt = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		updatedAt = pb.UpdatedAt.AsTime()
	}
	return &Task{
		ID:           pb.Id,
		Name:         pb.Name,
		Parent:       pb.Parent,
		Workspace:    pb.Workspace,
		Instructions: instructions,
		Status:       TaskStatus(pb.Status),
		Command:      TaskCommand(pb.Command),
		Version:      pb.Version,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
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

// Proto converts a Log to its protobuf LogEntry representation.
func (l *Log) Proto() *xagentv1.LogEntry {
	return &xagentv1.LogEntry{
		Type:    l.Type,
		Content: l.Content,
	}
}

// Proto converts a RunnerEvent to its protobuf representation.
func (r *RunnerEvent) Proto() *xagentv1.RunnerEvent {
	return &xagentv1.RunnerEvent{
		TaskId:    r.TaskID,
		Event:     string(r.Event),
		Version:   r.Version,
		Reconcile: r.Reconcile,
	}
}

// RunnerEventFromProto converts a protobuf RunnerEvent to a model RunnerEvent.
func RunnerEventFromProto(pb *xagentv1.RunnerEvent) RunnerEvent {
	return RunnerEvent{
		TaskID:    pb.TaskId,
		Event:     RunnerEventType(pb.Event),
		Version:   pb.Version,
		Reconcile: pb.Reconcile,
	}
}
