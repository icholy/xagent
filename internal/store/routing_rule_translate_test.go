package store

import (
	"testing"

	"github.com/icholy/xagent/internal/eventrouter2"
	"github.com/icholy/xagent/internal/model"
	"gotest.tools/v3/assert"
)

func TestDecodeRoutingRules(t *testing.T) {
	// An isolated registry with a few GitHub-shaped event types, so the legacy
	// fan-out has concrete types to translate onto without depending on the
	// process-wide DefaultSchemaRegistry.
	reg := eventrouter2.NewSchemaRegistry()
	reg.MustRegister(eventrouter2.EventTypeDef{Source: "github", Type: "issue_comment", Attrs: []string{"body", "url", "mention"}})
	reg.MustRegister(eventrouter2.EventTypeDef{Source: "github", Type: "issue_assigned", Attrs: []string{"body", "url", "assignee"}})
	reg.MustRegister(eventrouter2.EventTypeDef{Source: "github", Type: "label_added", Attrs: []string{"body", "url", "label"}})
	s := &Store{Registry: reg}

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
			rules, err := s.decodeRoutingRules([]byte(tt.data))
			assert.NilError(t, err)
			assert.DeepEqual(t, rules, tt.want)
		})
	}
}
