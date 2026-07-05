package store

import (
	"testing"

	"github.com/icholy/xagent/internal/eventrouter2"
	"github.com/icholy/xagent/internal/model"
	"gotest.tools/v3/assert"
)

// translateTestRegistry returns an isolated registry with a few GitHub-shaped
// event types, so the legacy fan-out has concrete types to translate onto
// without depending on the process-wide DefaultSchemaRegistry.
func translateTestRegistry() *eventrouter2.SchemaRegistry {
	reg := eventrouter2.NewSchemaRegistry()
	reg.MustRegister(eventrouter2.EventTypeDef{
		Source: "github", Type: "issue_comment",
		Attrs: []string{"body", "url", "mention"},
	})
	reg.MustRegister(eventrouter2.EventTypeDef{
		Source: "github", Type: "issue_assigned",
		Attrs: []string{"body", "url", "assignee"},
	})
	reg.MustRegister(eventrouter2.EventTypeDef{
		Source: "github", Type: "label_added",
		Attrs: []string{"body", "url", "label"},
	})
	return reg
}

func TestDecodeRoutingRulesLegacyConcrete(t *testing.T) {
	// A legacy rule naming a concrete (source, type) with a matcher field
	// translates to exactly one conditions rule.
	s := &Store{Registry: translateTestRegistry()}
	rules, err := s.decodeRoutingRules([]byte(`[{"source":"github","type":"issue_comment","mention":"alice","wakeup":true}]`))
	assert.NilError(t, err)
	assert.DeepEqual(t, rules, []model.RoutingRule{{
		Source:     "github",
		Type:       "issue_comment",
		Conditions: []model.Condition{{Attr: "mention", Op: "equals", Value: "alice"}},
		Wakeup:     true,
	}})
}

func TestDecodeRoutingRulesLegacyBodyPrefixFansOut(t *testing.T) {
	// A type-less legacy body-prefix rule fans out to every registered type,
	// since "body" is universal.
	s := &Store{Registry: translateTestRegistry()}
	rules, err := s.decodeRoutingRules([]byte(`[{"prefix":"xagent:","wakeup":true}]`))
	assert.NilError(t, err)
	cond := []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}}
	assert.DeepEqual(t, rules, []model.RoutingRule{
		{Source: "github", Type: "issue_comment", Conditions: cond, Wakeup: true},
		{Source: "github", Type: "issue_assigned", Conditions: cond, Wakeup: true},
		{Source: "github", Type: "label_added", Conditions: cond, Wakeup: true},
	})
}

func TestDecodeRoutingRulesLegacyValueBecomesLabel(t *testing.T) {
	// The legacy Value field translates to a "label" equals condition, so it
	// only applies to the type that emits "label".
	s := &Store{Registry: translateTestRegistry()}
	rules, err := s.decodeRoutingRules([]byte(`[{"value":"urgent"}]`))
	assert.NilError(t, err)
	assert.DeepEqual(t, rules, []model.RoutingRule{{
		Source:     "github",
		Type:       "label_added",
		Conditions: []model.Condition{{Attr: "label", Op: "equals", Value: "urgent"}},
	}})
}

func TestDecodeRoutingRulesConditionsPassthrough(t *testing.T) {
	// A conditions-native stored rule is decoded directly, untranslated.
	s := &Store{Registry: translateTestRegistry()}
	rules, err := s.decodeRoutingRules([]byte(`[{"source":"github","type":"issue_comment","conditions":[{"attr":"body","op":"prefix","value":"x:"}],"wakeup":true}]`))
	assert.NilError(t, err)
	assert.DeepEqual(t, rules, []model.RoutingRule{{
		Source:     "github",
		Type:       "issue_comment",
		Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "x:"}},
		Wakeup:     true,
	}})
}

func TestDecodeRoutingRulesBareRuleDecodedDirectly(t *testing.T) {
	// A bare source/type rule with neither legacy matchers nor conditions is
	// already interpretable as conditions, so it is decoded directly (nil
	// conditions) rather than fanned out.
	s := &Store{Registry: translateTestRegistry()}
	rules, err := s.decodeRoutingRules([]byte(`[{"source":"github","type":"issue_comment","wakeup":true}]`))
	assert.NilError(t, err)
	assert.DeepEqual(t, rules, []model.RoutingRule{{
		Source: "github",
		Type:   "issue_comment",
		Wakeup: true,
	}})
}

func TestDecodeRoutingRulesMixedRows(t *testing.T) {
	// Storage may hold a mix of legacy and conditions rows; each is decoded on
	// its own terms, preserving order.
	s := &Store{Registry: translateTestRegistry()}
	rules, err := s.decodeRoutingRules([]byte(`[
		{"source":"github","type":"issue_comment","mention":"alice"},
		{"source":"github","type":"label_added","conditions":[{"attr":"label","op":"equals","value":"bug"}]}
	]`))
	assert.NilError(t, err)
	assert.DeepEqual(t, rules, []model.RoutingRule{
		{Source: "github", Type: "issue_comment", Conditions: []model.Condition{{Attr: "mention", Op: "equals", Value: "alice"}}},
		{Source: "github", Type: "label_added", Conditions: []model.Condition{{Attr: "label", Op: "equals", Value: "bug"}}},
	})
}

func TestDecodeRoutingRulesEmpty(t *testing.T) {
	s := &Store{Registry: translateTestRegistry()}
	rules, err := s.decodeRoutingRules(nil)
	assert.NilError(t, err)
	assert.Equal(t, len(rules), 0)

	rules, err = s.decodeRoutingRules([]byte(`[]`))
	assert.NilError(t, err)
	assert.Equal(t, len(rules), 0)
}
