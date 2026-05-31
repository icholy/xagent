package model

import (
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
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
	Create    *CreateTaskAction `json:"create,omitempty"`
}

// CreateTaskAction configures a routing rule to create a new task on
// matching events. Workspace and runner identify where the task runs;
// the optional prompt is appended as a second instruction after the
// boilerplate event preamble.
type CreateTaskAction struct {
	Workspace string `json:"workspace"`
	Runner    string `json:"runner"`
	Prompt    string `json:"prompt,omitempty"`
}

// Proto converts a CreateTaskAction to its protobuf representation.
func (a *CreateTaskAction) Proto() *xagentv1.CreateTaskAction {
	return &xagentv1.CreateTaskAction{
		Workspace: a.Workspace,
		Runner:    a.Runner,
		Prompt:    a.Prompt,
	}
}

// CreateTaskActionFromProto converts a protobuf CreateTaskAction to the model type.
func CreateTaskActionFromProto(pb *xagentv1.CreateTaskAction) *CreateTaskAction {
	if pb == nil {
		return nil
	}
	return &CreateTaskAction{
		Workspace: pb.Workspace,
		Runner:    pb.Runner,
		Prompt:    pb.Prompt,
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
	}
	if r.Create != nil {
		pb.Create = r.Create.Proto()
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
		Create:    CreateTaskActionFromProto(pb.Create),
	}
}
