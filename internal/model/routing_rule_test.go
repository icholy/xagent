package model

import (
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestRoutingRuleProtoRoundTrip(t *testing.T) {
	rule := RoutingRule{
		Source:    "atlassian",
		Type:      "label_added",
		Prefix:    "xagent:",
		Mention:   "abc123",
		Assignee:  "icholy-bot",
		URLPrefix: "https://example.atlassian.net/browse/PROJ-",
		Value:     "xagent",
		Create: &CreateTaskAction{
			Workspace:    "default",
			Runner:       "runner-1",
			Prompt:       "do the thing",
			ArchiveAfter: time.Hour,
		},
		Wakeup: &Wakeup{Enable: false},
	}
	got := RoutingRuleFromProto(rule.Proto())
	assert.DeepEqual(t, got, rule)
}

func TestRoutingRuleProtoRoundTripWakeupEnabled(t *testing.T) {
	rule := RoutingRule{Source: "github", Wakeup: &Wakeup{Enable: true}}
	got := RoutingRuleFromProto(rule.Proto())
	assert.DeepEqual(t, got, rule)
}

func TestRoutingRuleShouldWake(t *testing.T) {
	// A nil Wakeup preserves the original behavior: wake.
	assert.Equal(t, RoutingRule{}.ShouldWake(), true)
	assert.Equal(t, RoutingRule{Wakeup: &Wakeup{Enable: true}}.ShouldWake(), true)
	assert.Equal(t, RoutingRule{Wakeup: &Wakeup{Enable: false}}.ShouldWake(), false)
}

func TestRoutingRuleProtoRoundTripValueOnly(t *testing.T) {
	rule := RoutingRule{Value: "urgent"}
	got := RoutingRuleFromProto(rule.Proto())
	assert.DeepEqual(t, got, rule)
}
