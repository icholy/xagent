package eventrouter

import (
	"testing"

	"github.com/icholy/xagent/internal/model"
	"gotest.tools/v3/assert"
)

func TestTranslateRuleConcreteType(t *testing.T) {
	// A concrete, registered (Source, Type) rule with an applicable condition
	// yields exactly one conditions rule.
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)

	rules := reg.TranslateRule(model.LegacyRoutingRule{
		Source:  "test",
		Type:    "comment",
		Mention: "alice",
	})
	assert.DeepEqual(t, rules, []model.RoutingRule{{
		Source:     "test",
		Type:       "comment",
		Conditions: []model.Condition{{Attr: "mention", Op: "equals", Value: "alice"}},
	}})
}

func TestTranslateRuleTypelessMention(t *testing.T) {
	// A type-less mention rule expands only to the type that emits "mention"
	// (test/comment); test/label and test/opened do not emit it.
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)
	reg.MustRegister(testLabel)
	reg.MustRegister(testOpened)

	rules := reg.TranslateRule(model.LegacyRoutingRule{Mention: "alice"})
	assert.DeepEqual(t, rules, []model.RoutingRule{{
		Source:     "test",
		Type:       "comment",
		Conditions: []model.Condition{{Attr: "mention", Op: "equals", Value: "alice"}},
	}})
}

func TestTranslateRuleTypelessBodyPrefix(t *testing.T) {
	// A type-less body-prefix rule expands to every fixture, since "body" is
	// universal.
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)
	reg.MustRegister(testLabel)
	reg.MustRegister(testOpened)

	rules := reg.TranslateRule(model.LegacyRoutingRule{Prefix: "xagent:"})
	cond := []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}}
	assert.DeepEqual(t, rules, []model.RoutingRule{
		{Source: "test", Type: "comment", Conditions: cond},
		{Source: "test", Type: "label", Conditions: cond},
		{Source: "test", Type: "opened", Conditions: cond},
	})
}

func TestTranslateRuleSourceOnly(t *testing.T) {
	// A source-only rule (no type, no conditions) expands to every type under
	// that source.
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)
	reg.MustRegister(testLabel)
	reg.MustRegister(testOpened)

	rules := reg.TranslateRule(model.LegacyRoutingRule{Source: "test"})
	assert.DeepEqual(t, rules, []model.RoutingRule{
		{Source: "test", Type: "comment"},
		{Source: "test", Type: "label"},
		{Source: "test", Type: "opened"},
	})
}

func TestTranslateRuleConditionAttrNotEmitted(t *testing.T) {
	// An assignee rule on a comment type produces zero results: the comment
	// fixture does not emit "assignee", matching v1 where such a rule silently
	// never matched.
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)

	rules := reg.TranslateRule(model.LegacyRoutingRule{
		Source:   "test",
		Type:     "comment",
		Assignee: "bob",
	})
	assert.Equal(t, len(rules), 0)
}

func TestTranslateRuleMultipleFields(t *testing.T) {
	// Multiple legacy fields become multiple conditions on the surviving type.
	// body + mention are both emitted by test/comment; label and other types
	// drop out.
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)
	reg.MustRegister(testLabel)
	reg.MustRegister(testOpened)

	rules := reg.TranslateRule(model.LegacyRoutingRule{
		Prefix:  "xagent:",
		Mention: "alice",
	})
	assert.DeepEqual(t, rules, []model.RoutingRule{{
		Source: "test",
		Type:   "comment",
		Conditions: []model.Condition{
			{Attr: "body", Op: "prefix", Value: "xagent:"},
			{Attr: "mention", Op: "equals", Value: "alice"},
		},
	}})
}

func TestTranslateRuleCarriesActions(t *testing.T) {
	// Wakeup and the Create pointer are carried through to every emitted rule.
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)

	create := &model.CreateTaskAction{Workspace: "ws", Runner: "rn", Prompt: "go"}
	rules := reg.TranslateRule(model.LegacyRoutingRule{
		Source: "test",
		Type:   "comment",
		Wakeup: true,
		Create: create,
	})
	assert.Equal(t, len(rules), 1)
	assert.Equal(t, rules[0].Wakeup, true)
	assert.Equal(t, rules[0].Create, create)
}

func TestTranslateRuleAllValid(t *testing.T) {
	// Every rule TranslateRule produces is a valid conditions rule.
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)
	reg.MustRegister(testLabel)
	reg.MustRegister(testOpened)

	legacy := []model.LegacyRoutingRule{
		{Source: "test", Type: "comment", Mention: "alice"},
		{Mention: "alice"},
		{Prefix: "xagent:"},
		{Source: "test"},
		{Value: "bug"},
		{URLPrefix: "https://github.com/icholy/xagent"},
	}
	for _, rule := range legacy {
		for _, out := range reg.TranslateRule(rule) {
			assert.NilError(t, reg.Validate(out))
		}
	}
}
