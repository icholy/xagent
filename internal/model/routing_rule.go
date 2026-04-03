package model

import (
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
)

// RoutingRule defines a routing rule that determines whether an event
// should be routed to an org's tasks. Empty fields are treated as wildcards.
type RoutingRule struct {
	Source  string `json:"source,omitempty"`
	Type    string `json:"type,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	Mention string `json:"mention,omitempty"`
}

// Proto converts a RoutingRule to its protobuf representation.
func (r *RoutingRule) Proto() *xagentv1.RoutingRule {
	return &xagentv1.RoutingRule{
		Source:  r.Source,
		Type:    r.Type,
		Prefix:  r.Prefix,
		Mention: r.Mention,
	}
}

// RoutingRuleFromProto converts a protobuf RoutingRule to the model type.
func RoutingRuleFromProto(pb *xagentv1.RoutingRule) RoutingRule {
	return RoutingRule{
		Source:  pb.Source,
		Type:    pb.Type,
		Prefix:  pb.Prefix,
		Mention: pb.Mention,
	}
}
