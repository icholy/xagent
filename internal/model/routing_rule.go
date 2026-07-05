package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

// RoutingRule defines a routing rule that determines whether an event should be
// routed to an org's tasks. A rule selects a (Source, Type) event kind — empty
// Source/Type are wildcards — and constrains it with a list of attribute
// Conditions (ANDed together). This is the conditions-native shape; pre-existing
// stored rules in the legacy flat-matcher shape are decoded via
// LegacyRoutingRule and translated on read.
type RoutingRule struct {
	Source     string            `json:"source,omitempty"`
	Type       string            `json:"type,omitempty"`
	Conditions []Condition       `json:"conditions,omitempty"`
	Create     *CreateTaskAction `json:"create,omitempty"`
	Wakeup     bool              `json:"wakeup,omitempty"`
}

// Condition constrains one attribute dimension of an event. Op is one of
// "equals", "prefix", or "contains"; comparisons are literal (exact,
// case-sensitive) string operations. It is the canonical conditions type the
// eventrouter2 matcher, registry, and store all operate on directly.
type Condition struct {
	Attr  string `json:"attr,omitempty"`
	Op    string `json:"op,omitempty"`
	Value string `json:"value,omitempty"`
}

// LegacyRoutingRule is the pre-conditions stored shape of a routing rule: the
// flat matcher fields that predate attribute conditions. It exists ONLY to
// decode rows written before the conditions cutover; those rows are translated
// to conditions-native RoutingRule(s) on read (see the store's translate-on-read
// path). Nothing writes this shape anymore.
type LegacyRoutingRule struct {
	Source    string            `json:"source,omitempty"`
	Type      string            `json:"type,omitempty"`
	Prefix    string            `json:"prefix,omitempty"`
	Mention   string            `json:"mention,omitempty"`
	Assignee  string            `json:"assignee,omitempty"`
	URLPrefix string            `json:"url_prefix,omitempty"`
	Value     string            `json:"value,omitempty"`
	Create    *CreateTaskAction `json:"create,omitempty"`
	Wakeup    bool              `json:"wakeup,omitempty"`
}

// HasMatcher reports whether the legacy rule carries any flat matcher field
// (Prefix/Mention/Assignee/URLPrefix/Value). It distinguishes a genuinely
// legacy-shaped stored rule — one that must be translated to conditions — from a
// bare source/type/wakeup/create rule, which is already interpretable directly
// as the conditions shape.
func (r LegacyRoutingRule) HasMatcher() bool {
	return r.Prefix != "" || r.Mention != "" || r.Assignee != "" || r.URLPrefix != "" || r.Value != ""
}

// CreateTaskAction configures a routing rule to create a new task on
// matching events. Workspace and runner identify where the task runs;
// the optional prompt is appended as a second instruction after the
// boilerplate event preamble.
type CreateTaskAction struct {
	Workspace string `json:"workspace"`
	Runner    string `json:"runner"`
	Prompt    string `json:"prompt,omitempty"`
	// AutoArchive is applied to the created task's auto-archive timeout.
	// See Task.AutoArchive for the value semantics (0 = never, <0 =
	// immediate, >0 = delay).
	AutoArchive time.Duration `json:"auto_archive,omitempty"`
}

// Proto converts a CreateTaskAction to its protobuf representation.
func (a *CreateTaskAction) Proto() *xagentv1.CreateTaskAction {
	return &xagentv1.CreateTaskAction{
		Workspace:   a.Workspace,
		Runner:      a.Runner,
		Prompt:      a.Prompt,
		AutoArchive: durationpb.New(a.AutoArchive),
	}
}

// CreateTaskActionFromProto converts a protobuf CreateTaskAction to the model type.
func CreateTaskActionFromProto(pb *xagentv1.CreateTaskAction) *CreateTaskAction {
	if pb == nil {
		return nil
	}
	return &CreateTaskAction{
		Workspace:   pb.Workspace,
		Runner:      pb.Runner,
		Prompt:      pb.Prompt,
		AutoArchive: pb.AutoArchive.AsDuration(),
	}
}

// Proto converts a RoutingRule to its protobuf representation.
func (r *RoutingRule) Proto() *xagentv1.RoutingRule {
	pb := &xagentv1.RoutingRule{
		Source: r.Source,
		Type:   r.Type,
		Wakeup: r.Wakeup,
	}
	for _, c := range r.Conditions {
		pb.Conditions = append(pb.Conditions, &xagentv1.RuleCondition{
			Attr:  c.Attr,
			Op:    c.Op,
			Value: c.Value,
		})
	}
	if r.Create != nil {
		pb.Create = r.Create.Proto()
	}
	return pb
}

// RoutingRuleFromProto converts a protobuf RoutingRule to the model type.
func RoutingRuleFromProto(pb *xagentv1.RoutingRule) RoutingRule {
	rule := RoutingRule{
		Source: pb.Source,
		Type:   pb.Type,
		Create: CreateTaskActionFromProto(pb.Create),
		Wakeup: pb.Wakeup,
	}
	for _, c := range pb.Conditions {
		rule.Conditions = append(rule.Conditions, Condition{
			Attr:  c.Attr,
			Op:    c.Op,
			Value: c.Value,
		})
	}
	return rule
}
