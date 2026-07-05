package eventrouter2

import "testing"

func TestRoutingRuleValidate(t *testing.T) {
	tests := []struct {
		name    string
		rule    RoutingRule
		wantErr bool
	}{
		{
			name: "default body-prefix rule",
			rule: RoutingRule{
				Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
				Wakeup:     true,
			},
		},
		{
			name: "mention equals on issue_comment",
			rule: RoutingRule{
				Source:     "github",
				Type:       "issue_comment",
				Conditions: []Condition{{Attr: "mention", Op: "equals", Value: "alice"}},
			},
		},
		{
			name: "label equals on label_added",
			rule: RoutingRule{
				Source:     "github",
				Type:       "label_added",
				Conditions: []Condition{{Attr: "label", Op: "equals", Value: "bug"}},
			},
		},
		{
			name: "any registered attr allowed when type is empty",
			rule: RoutingRule{
				Conditions: []Condition{{Attr: "assignee", Op: "equals", Value: "bob"}},
			},
		},
		{
			name: "url and body valid on any type",
			rule: RoutingRule{
				Source: "github",
				Type:   "pull_request_opened",
				Conditions: []Condition{
					{Attr: "url", Op: "contains", Value: "icholy/xagent"},
					{Attr: "body", Op: "contains", Value: "hi"},
				},
			},
		},
		{
			name: "unknown op",
			rule: RoutingRule{
				Conditions: []Condition{{Attr: "body", Op: "regex", Value: "x"}},
			},
			wantErr: true,
		},
		{
			name: "unknown attr",
			rule: RoutingRule{
				Conditions: []Condition{{Attr: "reviewer", Op: "equals", Value: "alice"}},
			},
			wantErr: true,
		},
		{
			name: "attr not emitted by selected type",
			rule: RoutingRule{
				Source:     "github",
				Type:       "issue_comment",
				Conditions: []Condition{{Attr: "assignee", Op: "equals", Value: "alice"}},
			},
			wantErr: true,
		},
		{
			name: "unknown event type",
			rule: RoutingRule{
				Source: "github",
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

func TestEventTypeFor(t *testing.T) {
	def, ok := EventTypeFor("github", "issue_comment")
	if !ok {
		t.Fatalf("EventTypeFor(github, issue_comment) = _, false; want hit")
	}
	if def.Label != "GitHub: Issue/PR Comment" {
		t.Errorf("Label = %q, want %q", def.Label, "GitHub: Issue/PR Comment")
	}

	if _, ok := EventTypeFor("github", "nope"); ok {
		t.Errorf("EventTypeFor(github, nope) = _, true; want miss")
	}
}
