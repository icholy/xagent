package store_test

import (
	"testing"

	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
	"gotest.tools/v3/assert"
)

func TestDecodeRoutingRules(t *testing.T) {
	// An isolated registry with a few GitHub-shaped event types, so the legacy
	// fan-out has concrete types to translate onto without depending on the
	// process-wide DefaultSchemaRegistry. It lives in eventrouter now, so this
	// test is external (package store_test) to import it without a cycle.
	reg := eventrouter.NewSchemaRegistry()
	reg.MustRegister(eventrouter.EventTypeDef{Source: "github", Type: "issue_comment", Attrs: attrDefs("body", "url", "mention")})
	reg.MustRegister(eventrouter.EventTypeDef{Source: "github", Type: "issue_assigned", Attrs: attrDefs("body", "url", "assignee")})
	reg.MustRegister(eventrouter.EventTypeDef{Source: "github", Type: "label_added", Attrs: attrDefs("body", "url", "label")})
	s := &store.Store{Rules: reg}

	bodyPrefix := []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}}
	tests := []struct {
		name string
		data string
		want []model.RoutingRule
	}{
		{
			// A legacy rule naming a concrete (source, type) with a matcher field
			// translates to exactly one conditions rule.
			name: "legacy concrete type",
			data: `[{"source":"github","type":"issue_comment","mention":"alice","wakeup":true}]`,
			want: []model.RoutingRule{{
				Source:     "github",
				Type:       "issue_comment",
				Conditions: []model.Condition{{Attr: "mention", Op: "equals", Value: "alice"}},
				Wakeup:     true,
			}},
		},
		{
			// A type-less legacy body-prefix rule fans out to every registered
			// type, since "body" is universal.
			name: "legacy body prefix fans out",
			data: `[{"prefix":"xagent:","wakeup":true}]`,
			want: []model.RoutingRule{
				{Source: "github", Type: "issue_comment", Conditions: bodyPrefix, Wakeup: true},
				{Source: "github", Type: "issue_assigned", Conditions: bodyPrefix, Wakeup: true},
				{Source: "github", Type: "label_added", Conditions: bodyPrefix, Wakeup: true},
			},
		},
		{
			// The legacy Value field translates to a "label" equals condition, so
			// it only applies to the type that emits "label".
			name: "legacy value becomes label",
			data: `[{"value":"urgent"}]`,
			want: []model.RoutingRule{{
				Source:     "github",
				Type:       "label_added",
				Conditions: []model.Condition{{Attr: "label", Op: "equals", Value: "urgent"}},
			}},
		},
		{
			// A conditions-native stored rule is decoded directly, untranslated.
			name: "conditions passthrough",
			data: `[{"source":"github","type":"issue_comment","conditions":[{"attr":"body","op":"prefix","value":"x:"}],"wakeup":true}]`,
			want: []model.RoutingRule{{
				Source:     "github",
				Type:       "issue_comment",
				Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "x:"}},
				Wakeup:     true,
			}},
		},
		{
			// A bare source/type rule with neither legacy matchers nor conditions
			// is already interpretable as conditions, so it is decoded directly
			// (nil conditions) rather than fanned out.
			name: "bare rule decoded directly",
			data: `[{"source":"github","type":"issue_comment","wakeup":true}]`,
			want: []model.RoutingRule{{
				Source: "github",
				Type:   "issue_comment",
				Wakeup: true,
			}},
		},
		{
			// Storage may hold a mix of legacy and conditions rows; each is decoded
			// on its own terms, preserving order.
			name: "mixed legacy and conditions rows",
			data: `[
				{"source":"github","type":"issue_comment","mention":"alice"},
				{"source":"github","type":"label_added","conditions":[{"attr":"label","op":"equals","value":"bug"}]}
			]`,
			want: []model.RoutingRule{
				{Source: "github", Type: "issue_comment", Conditions: []model.Condition{{Attr: "mention", Op: "equals", Value: "alice"}}},
				{Source: "github", Type: "label_added", Conditions: []model.Condition{{Attr: "label", Op: "equals", Value: "bug"}}},
			},
		},
		{
			name: "empty json array",
			data: `[]`,
			want: nil,
		},
		{
			name: "no data",
			data: ``,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules, err := s.DecodeRoutingRules([]byte(tt.data))
			assert.NilError(t, err)
			assert.DeepEqual(t, rules, tt.want)
		})
	}
}

// TestDecodeRoutingRulesLegacyWithoutTranslator verifies the no-default rule:
// a legacy row requires a translator, so decoding one while Rules is nil is an
// error rather than a silent drop. Conditions-native and bare rows still decode.
func TestDecodeRoutingRulesLegacyWithoutTranslator(t *testing.T) {
	s := &store.Store{} // Rules nil

	// A legacy (flat-matcher) row errors without a translator.
	_, err := s.DecodeRoutingRules([]byte(`[{"prefix":"xagent:","wakeup":true}]`))
	assert.ErrorContains(t, err, "store.Rules is nil")

	// A conditions-native row decodes fine without a translator.
	rules, err := s.DecodeRoutingRules([]byte(`[{"source":"github","type":"issue_comment","conditions":[{"attr":"body","op":"prefix","value":"x:"}]}]`))
	assert.NilError(t, err)
	assert.DeepEqual(t, rules, []model.RoutingRule{{
		Source:     "github",
		Type:       "issue_comment",
		Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "x:"}},
	}})

	// A bare source/type row decodes fine too.
	rules, err = s.DecodeRoutingRules([]byte(`[{"source":"github","type":"issue_comment","wakeup":true}]`))
	assert.NilError(t, err)
	assert.DeepEqual(t, rules, []model.RoutingRule{{
		Source: "github",
		Type:   "issue_comment",
		Wakeup: true,
	}})
}

// attrDefs builds a bare AttrDef slice from attr keys — the translate/decode
// tests only care about the attr keys, not their display copy.
func attrDefs(keys ...string) []eventrouter.AttrDef {
	defs := make([]eventrouter.AttrDef, len(keys))
	for i, key := range keys {
		defs[i] = eventrouter.AttrDef{Key: key}
	}
	return defs
}
