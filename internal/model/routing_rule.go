package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

// RoutingRule defines a routing rule that determines whether an event
// should be routed to an org's tasks. Empty fields are treated as wildcards.
type RoutingRule struct {
	Source    string            `json:"source,omitempty"`
	Type      string            `json:"type,omitempty"`
	Prefix    string            `json:"prefix,omitempty"`
	Mention   string            `json:"mention,omitempty"`
	Assignee  string            `json:"assignee,omitempty"`
	URLPrefix string            `json:"url_prefix,omitempty"`
	Value     string            `json:"value,omitempty"`
	Create    *CreateTaskAction `json:"create,omitempty"`
	// Wakeup controls whether a matched rule restarts (wakes) the linked
	// task(s). A nil Wakeup is treated as Enable: true — i.e. wake, the current
	// behavior — so existing rules keep working unchanged.
	Wakeup *Wakeup `json:"wakeup,omitempty"`
}

// Wakeup configures whether a matched routing rule wakes the linked task(s).
// Modeled as an extensible nested struct (like CreateTaskAction) so filter
// fields can be added later.
type Wakeup struct {
	// Enable, when false, means the rule attaches the event and emits a channel
	// notification but does NOT restart the linked task(s).
	Enable bool `json:"enable"`
}

// ShouldWake reports whether a matched rule should wake (restart) the linked
// task(s). A nil Wakeup defaults to waking, preserving the original behavior.
func (r RoutingRule) ShouldWake() bool {
	return r.Wakeup == nil || r.Wakeup.Enable
}

// Proto converts a Wakeup to its protobuf representation.
func (w *Wakeup) Proto() *xagentv1.Wakeup {
	return &xagentv1.Wakeup{
		Enable: w.Enable,
	}
}

// WakeupFromProto converts a protobuf Wakeup to the model type.
func WakeupFromProto(pb *xagentv1.Wakeup) *Wakeup {
	if pb == nil {
		return nil
	}
	return &Wakeup{
		Enable: pb.Enable,
	}
}

// CreateTaskAction configures a routing rule to create a new task on
// matching events. Workspace and runner identify where the task runs;
// the optional prompt is appended as a second instruction after the
// boilerplate event preamble.
type CreateTaskAction struct {
	Workspace string `json:"workspace"`
	Runner    string `json:"runner"`
	Prompt    string `json:"prompt,omitempty"`
	// ArchiveAfter is applied to the created task's auto-archive timeout.
	// See Task.ArchiveAfter for the value semantics (0 = never, <0 =
	// immediate, >0 = delay).
	ArchiveAfter time.Duration `json:"archive_after,omitempty"`
}

// Proto converts a CreateTaskAction to its protobuf representation.
func (a *CreateTaskAction) Proto() *xagentv1.CreateTaskAction {
	return &xagentv1.CreateTaskAction{
		Workspace:    a.Workspace,
		Runner:       a.Runner,
		Prompt:       a.Prompt,
		ArchiveAfter: durationpb.New(a.ArchiveAfter),
	}
}

// CreateTaskActionFromProto converts a protobuf CreateTaskAction to the model type.
func CreateTaskActionFromProto(pb *xagentv1.CreateTaskAction) *CreateTaskAction {
	if pb == nil {
		return nil
	}
	return &CreateTaskAction{
		Workspace:    pb.Workspace,
		Runner:       pb.Runner,
		Prompt:       pb.Prompt,
		ArchiveAfter: pb.ArchiveAfter.AsDuration(),
	}
}

// Proto converts a RoutingRule to its protobuf representation.
func (r *RoutingRule) Proto() *xagentv1.RoutingRule {
	pb := &xagentv1.RoutingRule{
		Source:    r.Source,
		Type:      r.Type,
		Prefix:    r.Prefix,
		Mention:   r.Mention,
		Assignee:  r.Assignee,
		UrlPrefix: r.URLPrefix,
		Value:     r.Value,
	}
	if r.Create != nil {
		pb.Create = r.Create.Proto()
	}
	if r.Wakeup != nil {
		pb.Wakeup = r.Wakeup.Proto()
	}
	return pb
}

// RoutingRuleFromProto converts a protobuf RoutingRule to the model type.
func RoutingRuleFromProto(pb *xagentv1.RoutingRule) RoutingRule {
	return RoutingRule{
		Source:    pb.Source,
		Type:      pb.Type,
		Prefix:    pb.Prefix,
		Mention:   pb.Mention,
		Assignee:  pb.Assignee,
		URLPrefix: pb.UrlPrefix,
		Value:     pb.Value,
		Create:    CreateTaskActionFromProto(pb.Create),
		Wakeup:    WakeupFromProto(pb.Wakeup),
	}
}
