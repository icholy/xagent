package model

import (
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

// RoutingRule defines a routing rule that determines whether an event
// should be routed to an org's tasks. Empty fields are treated as wildcards.
type RoutingRule struct {
	Source  string            `json:"source,omitempty"`
	Type    string            `json:"type,omitempty"`
	Prefix  string            `json:"prefix,omitempty"`
	Mention string            `json:"mention,omitempty"`
	Create  *CreateTaskAction `json:"create,omitempty"`
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

// Proto converts a RoutingRule to its protobuf representation.
func (r *RoutingRule) Proto() *xagentv1.RoutingRule {
	pb := &xagentv1.RoutingRule{
		Source:  r.Source,
		Type:    r.Type,
		Prefix:  r.Prefix,
		Mention: r.Mention,
	}
	if r.Create != nil {
		pb.Create = &xagentv1.CreateTaskAction{
			Workspace: r.Create.Workspace,
			Runner:    r.Create.Runner,
			Prompt:    r.Create.Prompt,
		}
	}
	return pb
}

// RoutingRuleFromProto converts a protobuf RoutingRule to the model type.
func RoutingRuleFromProto(pb *xagentv1.RoutingRule) RoutingRule {
	rule := RoutingRule{
		Source:  pb.Source,
		Type:    pb.Type,
		Prefix:  pb.Prefix,
		Mention: pb.Mention,
	}
	if pb.Create != nil {
		rule.Create = &CreateTaskAction{
			Workspace: pb.Create.Workspace,
			Runner:    pb.Create.Runner,
			Prompt:    pb.Create.Prompt,
		}
	}
	return rule
}
