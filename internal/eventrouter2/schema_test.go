package eventrouter2

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// testRegistry returns an isolated registry populated with clearly-synthetic
// schemas under the fake "test" source. Each test builds its own registry via
// this helper rather than mutating a package global, so the package's tests stay
// self-contained — they exercise the registry through these fixtures rather than
// importing the producer packages that register the real github/atlassian
// schemas. Only test/comment ships a default rule, mirroring the comment/review
// types in production.
func testRegistry() *SchemaRegistry {
	reg := NewSchemaRegistry()
	reg.MustRegister(EventTypeDef{
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
	reg.MustRegister(EventTypeDef{
		Source: "test",
		Type:   "label",
		Label:  "Test: Label",
		Attrs:  []string{"body", "url", "label"},
	})
	reg.MustRegister(EventTypeDef{
		Source: "test",
		Type:   "opened",
		Label:  "Test: Opened",
		Attrs:  []string{"body", "url"},
	})
	return reg
}

func TestSchemaRegistryValidate(t *testing.T) {
	reg := testRegistry()
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
			err := reg.Validate(tt.rule)
			if tt.wantErr {
				assert.Assert(t, err != nil, "Validate() = nil, want error")
			} else {
				assert.NilError(t, err)
			}
		})
	}
}

func TestSchemaRegistryEventTypeFor(t *testing.T) {
	reg := testRegistry()

	def, ok := reg.EventTypeFor("test", "comment")
	assert.Assert(t, ok, "EventTypeFor(test, comment) = _, false; want hit")
	assert.Equal(t, def.Label, "Test: Comment")

	_, ok = reg.EventTypeFor("test", "nope")
	assert.Assert(t, !ok, "EventTypeFor(test, nope) = _, true; want miss")
}

func TestSchemaRegistryEventTypes(t *testing.T) {
	reg := testRegistry()
	defs := reg.EventTypes()
	assert.Equal(t, len(defs), 3)
	assert.Equal(t, defs[0].Type, "comment")
	assert.Equal(t, defs[1].Type, "label")
	assert.Equal(t, defs[2].Type, "opened")
}

func TestSchemaRegistryDefaultRules(t *testing.T) {
	reg := testRegistry()
	// Only test/comment ships a default rule; the accumulated set carries it in
	// registration order.
	assert.DeepEqual(t, reg.DefaultRules(), []RoutingRule{{
		Source:     "test",
		Type:       "comment",
		Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
		Wakeup:     true,
	}})
}

func TestSchemaRegistryMustRegisterDuplicatePanics(t *testing.T) {
	reg := testRegistry()
	// test/comment is already registered; re-registering the same (source, type)
	// must panic.
	assert.Assert(t, cmp.Panics(func() {
		reg.MustRegister(EventTypeDef{Source: "test", Type: "comment", Attrs: []string{"body"}})
	}))
}
