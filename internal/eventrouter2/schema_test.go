package eventrouter2

import "testing"

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
			if tt.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestDefaultRules(t *testing.T) {
	rules := DefaultRules()
	if len(rules) == 0 {
		t.Fatal("DefaultRules() returned no rules")
	}
	for i, rule := range rules {
		if err := rule.Validate(); err != nil {
			t.Errorf("DefaultRules()[%d] (source=%q type=%q) failed Validate: %v", i, rule.Source, rule.Type, err)
		}
	}
	// test/comment is the only fixture carrying a default rule; DefaultRules()
	// must surface it and no rule from a fixture that ships none (test/label).
	var sawComment bool
	for _, rule := range rules {
		if rule.Source == "test" && rule.Type == "comment" {
			sawComment = true
		}
		if rule.Type == "label" {
			t.Errorf("DefaultRules() surfaced a rule for %q, which ships no defaults", rule.Type)
		}
	}
	if !sawComment {
		t.Error("DefaultRules() missing the test/comment default rule")
	}
}

func TestEventTypes(t *testing.T) {
	byKey := map[string]EventTypeDef{}
	for _, def := range EventTypes() {
		byKey[def.Source+":"+def.Type] = def
	}
	for _, want := range []string{"test:comment", "test:label", "test:opened"} {
		if _, ok := byKey[want]; !ok {
			t.Errorf("EventTypes() missing %q", want)
		}
	}
}

func TestEventTypeFor(t *testing.T) {
	def, ok := EventTypeFor("test", "comment")
	if !ok {
		t.Fatalf("EventTypeFor(test, comment) = _, false; want hit")
	}
	if def.Label != "Test: Comment" {
		t.Errorf("Label = %q, want %q", def.Label, "Test: Comment")
	}

	if _, ok := EventTypeFor("test", "nope"); ok {
		t.Errorf("EventTypeFor(test, nope) = _, true; want miss")
	}
}

func TestMustRegisterSchemaDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustRegisterSchema did not panic on duplicate (source, type)")
		}
	}()
	// test/comment is already registered by the fixtures; re-registering the
	// same (source, type) must panic before mutating the registry.
	MustRegisterSchema(EventTypeDef{Source: "test", Type: "comment", Attrs: []string{"body"}})
}
