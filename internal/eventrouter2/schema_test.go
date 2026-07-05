package eventrouter2

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// Test fixtures: clearly-synthetic schemas registered under the fake "test"
// source. The package's tests stay self-contained — they exercise the registry
// through these fixtures rather than importing the producer packages that
// register the real github/atlassian schemas. Only test/comment ships a default
// rule, mirroring the comment/review types in production.
func init() {
	MustRegisterSchema(EventTypeDef{
		Source: "test",
		Type:   "comment",
		Label:  "Test: Comment",
		Attrs:  []string{"body", "url", "mention"},
		DefaultRules: []RoutingRule{{
			Source:     "test",
			Type:       "comment",
			Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
			Wakeup:     true,
		}},
	})
	MustRegisterSchema(EventTypeDef{
		Source: "test",
		Type:   "label",
		Label:  "Test: Label",
		Attrs:  []string{"body", "url", "label"},
	})
	MustRegisterSchema(EventTypeDef{
		Source: "test",
		Type:   "opened",
		Label:  "Test: Opened",
		Attrs:  []string{"body", "url"},
	})
}

func TestRoutingRuleValidate(t *testing.T) {
	tests := []struct {
		name    string
		rule    RoutingRule
		wantErr bool
	}{
		{
			name: "default body-prefix rule",
			rule: RoutingRule{
				Source:     "test",
				Type:       "comment",
				Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
				Wakeup:     true,
			},
		},
		{
			name: "mention equals on comment",
			rule: RoutingRule{
				Source:     "test",
				Type:       "comment",
				Conditions: []Condition{{Attr: "mention", Op: "equals", Value: "alice"}},
			},
		},
		{
			name: "label equals on label",
			rule: RoutingRule{
				Source:     "test",
				Type:       "label",
				Conditions: []Condition{{Attr: "label", Op: "equals", Value: "bug"}},
			},
		},
		{
			name: "empty type is rejected",
			rule: RoutingRule{
				Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
			},
			wantErr: true,
		},
		{
			name: "empty source is rejected",
			rule: RoutingRule{
				Type:       "comment",
				Conditions: []Condition{{Attr: "mention", Op: "equals", Value: "alice"}},
			},
			wantErr: true,
		},
		{
			name: "url and body valid on any type",
			rule: RoutingRule{
				Source: "test",
				Type:   "opened",
				Conditions: []Condition{
					{Attr: "url", Op: "contains", Value: "icholy/xagent"},
					{Attr: "body", Op: "contains", Value: "hi"},
				},
			},
		},
		{
			name: "unknown op",
			rule: RoutingRule{
				Source:     "test",
				Type:       "comment",
				Conditions: []Condition{{Attr: "body", Op: "regex", Value: "x"}},
			},
			wantErr: true,
		},
		{
			name: "unknown attr",
			rule: RoutingRule{
				Source:     "test",
				Type:       "comment",
				Conditions: []Condition{{Attr: "reviewer", Op: "equals", Value: "alice"}},
			},
			wantErr: true,
		},
		{
			name: "attr not emitted by selected type",
			rule: RoutingRule{
				Source:     "test",
				Type:       "comment",
				Conditions: []Condition{{Attr: "label", Op: "equals", Value: "bug"}},
			},
			wantErr: true,
		},
		{
			name: "unknown event type",
			rule: RoutingRule{
				Source: "test",
				Type:   "star_added",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if tt.wantErr {
				assert.Assert(t, err != nil, "Validate() = nil, want error")
			} else {
				assert.NilError(t, err)
			}
		})
	}
}

func TestDefaultRules(t *testing.T) {
	rules := DefaultRules()
	assert.Assert(t, len(rules) > 0, "DefaultRules() returned no rules")
	// test/comment is the only fixture carrying a default rule; DefaultRules()
	// must surface it and no rule from a fixture that ships none (test/label).
	var sawComment bool
	for i, rule := range rules {
		assert.NilError(t, rule.Validate(), "DefaultRules()[%d] (source=%q type=%q)", i, rule.Source, rule.Type)
		if rule.Source == "test" && rule.Type == "comment" {
			sawComment = true
		}
		assert.Assert(t, rule.Type != "label", "DefaultRules() surfaced a rule for %q, which ships no defaults", rule.Type)
	}
	assert.Assert(t, sawComment, "DefaultRules() missing the test/comment default rule")
}

func TestEventTypes(t *testing.T) {
	byKey := map[string]EventTypeDef{}
	for _, def := range EventTypes() {
		byKey[def.Source+":"+def.Type] = def
	}
	for _, want := range []string{"test:comment", "test:label", "test:opened"} {
		_, ok := byKey[want]
		assert.Assert(t, ok, "EventTypes() missing %q", want)
	}
}

func TestEventTypeFor(t *testing.T) {
	def, ok := EventTypeFor("test", "comment")
	assert.Assert(t, ok, "EventTypeFor(test, comment) = _, false; want hit")
	assert.Equal(t, def.Label, "Test: Comment")

	_, ok = EventTypeFor("test", "nope")
	assert.Assert(t, !ok, "EventTypeFor(test, nope) = _, true; want miss")
}

func TestMustRegisterSchemaDuplicatePanics(t *testing.T) {
	// test/comment is already registered by the fixtures; re-registering the
	// same (source, type) must panic before mutating the registry.
	assert.Assert(t, cmp.Panics(func() {
		MustRegisterSchema(EventTypeDef{Source: "test", Type: "comment", Attrs: []string{"body"}})
	}))
}
