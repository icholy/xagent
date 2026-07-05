package model

import (
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestRoutingRuleProtoRoundTrip(t *testing.T) {
	rule := RoutingRule{
		Source: "atlassian",
		Type:   "label_added",
		Conditions: []Condition{
			{Attr: "body", Op: "prefix", Value: "xagent:"},
			{Attr: "mention", Op: "equals", Value: "abc123"},
			{Attr: "label", Op: "equals", Value: "xagent"},
		},
		Create: &CreateTaskAction{
			Workspace:   "default",
			Runner:      "runner-1",
			Prompt:      "do the thing",
			AutoArchive: time.Hour,
		},
		Wakeup: true,
	}
	got := RoutingRuleFromProto(rule.Proto())
	assert.DeepEqual(t, got, rule)
}

func TestRoutingRuleProtoRoundTripWakeupDisabled(t *testing.T) {
	rule := RoutingRule{Source: "github", Wakeup: false}
	got := RoutingRuleFromProto(rule.Proto())
	assert.DeepEqual(t, got, rule)
}

func TestRoutingRuleProtoRoundTripConditionsOnly(t *testing.T) {
	rule := RoutingRule{
		Source:     "atlassian",
		Type:       "label_added",
		Conditions: []Condition{{Attr: "label", Op: "equals", Value: "urgent"}},
	}
	got := RoutingRuleFromProto(rule.Proto())
	assert.DeepEqual(t, got, rule)
}
