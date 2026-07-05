package eventrouter

import (
	"testing"

	"github.com/icholy/xagent/internal/model"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// Clearly-synthetic schema fixtures under the fake "test" source. The package's
// tests register only the ones each case needs on an isolated registry, so they
// stay self-contained rather than importing the producer packages that register
// the real github/atlassian schemas. Only test/comment ships a default rule,
// mirroring the comment/review types in production.
var (
	testComment = EventTypeDef{
		Source: "test",
		Type:   "comment",
		Label:  "Test: Comment",
		Attrs:  attrDefs("body", "url", "mention"),
		DefaultRules: []model.RoutingRule{{
			Source:     "test",
			Type:       "comment",
			Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
			Wakeup:     true,
		}},
	}
	testLabel = EventTypeDef{
		Source: "test",
		Type:   "label",
		Label:  "Test: Label",
		Attrs:  attrDefs("body", "url", "label"),
	}
	testOpened = EventTypeDef{
		Source: "test",
		Type:   "opened",
		Label:  "Test: Opened",
		Attrs:  attrDefs("body", "url"),
	}
)

// attrDefs builds a bare AttrDef slice from attr keys for the synthetic test
// schemas — the display copy is irrelevant to the router/validation tests, so
// only Key is set.
func attrDefs(keys ...string) []AttrDef {
	defs := make([]AttrDef, len(keys))
	for i, key := range keys {
		defs[i] = AttrDef{Key: key}
	}
	return defs
}

func TestSchemaRegistryValidate(t *testing.T) {
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)
	reg.MustRegister(testLabel)
	reg.MustRegister(testOpened)

	tests := []struct {
		name    string
		rule    model.RoutingRule
		wantErr bool
	}{
		{
			name: "default body-prefix rule",
			rule: model.RoutingRule{
				Source:     "test",
				Type:       "comment",
				Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
				Wakeup:     true,
			},
		},
		{
			name: "mention equals on comment",
			rule: model.RoutingRule{
				Source:     "test",
				Type:       "comment",
				Conditions: []model.Condition{{Attr: "mention", Op: "equals", Value: "alice"}},
			},
		},
		{
			name: "label equals on label",
			rule: model.RoutingRule{
				Source:     "test",
				Type:       "label",
				Conditions: []model.Condition{{Attr: "label", Op: "equals", Value: "bug"}},
			},
		},
		{
			name: "empty type is rejected",
			rule: model.RoutingRule{
				Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
			},
			wantErr: true,
		},
		{
			name: "empty source is rejected",
			rule: model.RoutingRule{
				Type:       "comment",
				Conditions: []model.Condition{{Attr: "mention", Op: "equals", Value: "alice"}},
			},
			wantErr: true,
		},
		{
			name: "url and body valid on any type",
			rule: model.RoutingRule{
				Source: "test",
				Type:   "opened",
				Conditions: []model.Condition{
					{Attr: "url", Op: "contains", Value: "icholy/xagent"},
					{Attr: "body", Op: "contains", Value: "hi"},
				},
			},
		},
		{
			name: "unknown op",
			rule: model.RoutingRule{
				Source:     "test",
				Type:       "comment",
				Conditions: []model.Condition{{Attr: "body", Op: "regex", Value: "x"}},
			},
			wantErr: true,
		},
		{
			name: "unknown attr",
			rule: model.RoutingRule{
				Source:     "test",
				Type:       "comment",
				Conditions: []model.Condition{{Attr: "reviewer", Op: "equals", Value: "alice"}},
			},
			wantErr: true,
		},
		{
			name: "attr not emitted by selected type",
			rule: model.RoutingRule{
				Source:     "test",
				Type:       "comment",
				Conditions: []model.Condition{{Attr: "label", Op: "equals", Value: "bug"}},
			},
			wantErr: true,
		},
		{
			name: "unknown event type",
			rule: model.RoutingRule{
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
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)

	def, ok := reg.EventTypeFor("test", "comment")
	assert.Assert(t, ok, "EventTypeFor(test, comment) = _, false; want hit")
	assert.Equal(t, def.Label, "Test: Comment")

	_, ok = reg.EventTypeFor("test", "nope")
	assert.Assert(t, !ok, "EventTypeFor(test, nope) = _, true; want miss")
}

func TestSchemaRegistryEventTypes(t *testing.T) {
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)
	reg.MustRegister(testLabel)
	reg.MustRegister(testOpened)

	defs := reg.EventTypes()
	assert.Equal(t, len(defs), 3)
	assert.Equal(t, defs[0].Type, "comment")
	assert.Equal(t, defs[1].Type, "label")
	assert.Equal(t, defs[2].Type, "opened")
}

func TestSchemaRegistryDefaultRules(t *testing.T) {
	reg := NewSchemaRegistry()
	// test/comment ships a default rule; test/opened does not, so only the
	// comment rule is accumulated.
	reg.MustRegister(testComment)
	reg.MustRegister(testOpened)

	assert.DeepEqual(t, reg.DefaultRules(), []model.RoutingRule{{
		Source:     "test",
		Type:       "comment",
		Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
		Wakeup:     true,
	}})
}

func TestSchemaRegistryMustRegisterDuplicatePanics(t *testing.T) {
	reg := NewSchemaRegistry()
	reg.MustRegister(testComment)
	// Re-registering the same (source, type) must panic.
	assert.Assert(t, cmp.Panics(func() {
		reg.MustRegister(EventTypeDef{Source: "test", Type: "comment", Attrs: attrDefs("body")})
	}))
}
