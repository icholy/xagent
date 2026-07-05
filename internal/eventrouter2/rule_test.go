package eventrouter2

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestMatchRule(t *testing.T) {
	tests := []struct {
		name  string
		rule  RoutingRule
		event InputEvent
		want  bool
	}{
		// --- selector: source/type ---
		{
			name:  "empty rule matches everything",
			rule:  RoutingRule{},
			event: InputEvent{Source: "github", Type: "issue_comment", Data: "hello"},
			want:  true,
		},
		{
			name:  "source match",
			rule:  RoutingRule{Source: "github"},
			event: InputEvent{Source: "github"},
			want:  true,
		},
		{
			name:  "source mismatch",
			rule:  RoutingRule{Source: "github"},
			event: InputEvent{Source: "atlassian"},
			want:  false,
		},
		{
			name:  "type match",
			rule:  RoutingRule{Type: "issue_comment"},
			event: InputEvent{Source: "github", Type: "issue_comment"},
			want:  true,
		},
		{
			name:  "type mismatch",
			rule:  RoutingRule{Type: "issue_comment"},
			event: InputEvent{Source: "github", Type: "pull_request_review"},
			want:  false,
		},
		{
			name:  "empty source is a wildcard",
			rule:  RoutingRule{Type: "issue_comment"},
			event: InputEvent{Source: "atlassian", Type: "issue_comment"},
			want:  true,
		},
		{
			name:  "empty type is a wildcard",
			rule:  RoutingRule{Source: "github"},
			event: InputEvent{Source: "github", Type: "anything"},
			want:  true,
		},

		// --- Prefix -> {body, prefix} ---
		{
			name:  "prefix match",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}}},
			event: InputEvent{Data: "xagent: fix the tests"},
			want:  true,
		},
		{
			name:  "prefix mismatch",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}}},
			event: InputEvent{Data: "just a comment"},
			want:  false,
		},

		// --- Mention -> {mention, equals} ---
		// The event now supplies extracted mentions in Attrs["mention"]
		// rather than the router parsing @-syntax out of Data.
		{
			name:  "github mention match",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "mention", Op: "equals", Value: "botuser"}}},
			event: InputEvent{Source: "github", Attrs: Attrs{"mention": {"botuser"}}},
			want:  true,
		},
		{
			// DELTA: legacy Mention matching was case-insensitive; conditions
			// are literal, so BotUser no longer matches an extracted "botuser".
			name:  "github mention is now case sensitive",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "mention", Op: "equals", Value: "BotUser"}}},
			event: InputEvent{Source: "github", Attrs: Attrs{"mention": {"botuser"}}},
			want:  false,
		},
		{
			name:  "github mention no substring match",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "mention", Op: "equals", Value: "bot"}}},
			event: InputEvent{Source: "github", Attrs: Attrs{"mention": {"botuser"}}},
			want:  false,
		},
		{
			name:  "github mention mismatch",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "mention", Op: "equals", Value: "botuser"}}},
			event: InputEvent{Source: "github", Attrs: Attrs{"mention": {"someoneelse"}}},
			want:  false,
		},
		{
			name:  "atlassian mention match",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "mention", Op: "equals", Value: "abc123"}}},
			event: InputEvent{Source: "atlassian", Attrs: Attrs{"mention": {"abc123"}}},
			want:  true,
		},
		{
			name:  "atlassian mention mismatch",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "mention", Op: "equals", Value: "abc123"}}},
			event: InputEvent{Source: "atlassian", Attrs: Attrs{"mention": {"xyz"}}},
			want:  false,
		},
		{
			// A mention condition against an event that carries no mention
			// attr fails, regardless of what's in Data.
			name:  "mention condition on event without mention attr fails",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "mention", Op: "equals", Value: "user"}}},
			event: InputEvent{Source: "unknown", Data: "@user hello"},
			want:  false,
		},

		// --- Assignee -> {assignee, equals} ---
		{
			name:  "github assignee match",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "assignee", Op: "equals", Value: "icholy-bot"}}},
			event: InputEvent{Source: "github", Type: "pull_request_assigned", Attrs: Attrs{"assignee": {"icholy-bot"}}},
			want:  true,
		},
		{
			// DELTA: legacy Assignee matching was case-insensitive; conditions
			// are literal, so Icholy-Bot no longer matches "icholy-bot".
			name:  "github assignee is now case sensitive",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "assignee", Op: "equals", Value: "icholy-bot"}}},
			event: InputEvent{Source: "github", Type: "pull_request_assigned", Attrs: Attrs{"assignee": {"Icholy-Bot"}}},
			want:  false,
		},
		{
			name:  "github assignee mismatch",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "assignee", Op: "equals", Value: "icholy-bot"}}},
			event: InputEvent{Source: "github", Type: "pull_request_assigned", Attrs: Attrs{"assignee": {"octocat"}}},
			want:  false,
		},
		{
			// Replaces the legacy "assignee empty on non-assignment event"
			// case: an event that carries no assignee attr fails the condition.
			name:  "assignee condition on event without assignee attr fails",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "assignee", Op: "equals", Value: "icholy-bot"}}},
			event: InputEvent{Source: "github", Type: "issue_comment", Data: "hi"},
			want:  false,
		},

		// --- URLPrefix -> {url, prefix} ---
		{
			name:  "empty url prefix (no condition) matches any url",
			rule:  RoutingRule{},
			event: InputEvent{URL: "https://github.com/icholy/xagent/pull/1"},
			want:  true,
		},
		{
			name:  "url prefix match",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "url", Op: "prefix", Value: "https://github.com/icholy/xagent/"}}},
			event: InputEvent{URL: "https://github.com/icholy/xagent/pull/1"},
			want:  true,
		},
		{
			name:  "url prefix mismatch",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "url", Op: "prefix", Value: "https://github.com/icholy/xagent/"}}},
			event: InputEvent{URL: "https://github.com/other/repo/pull/1"},
			want:  false,
		},
		{
			name:  "url prefix mismatch on empty url",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "url", Op: "prefix", Value: "https://github.com/icholy/xagent/"}}},
			event: InputEvent{URL: ""},
			want:  false,
		},
		{
			name: "url prefix is independent of body",
			rule: RoutingRule{Conditions: []Condition{{Attr: "url", Op: "prefix", Value: "https://github.com/icholy/xagent/"}}},
			event: InputEvent{
				URL:  "https://github.com/icholy/xagent/issues/1",
				Data: "no xagent: prefix here",
			},
			want: true,
		},
		{
			name: "body prefix is independent of url",
			rule: RoutingRule{Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}}},
			event: InputEvent{
				URL:  "https://example.com/whatever",
				Data: "xagent: do the thing",
			},
			want: true,
		},

		// --- Value -> {label, equals} ---
		{
			name:  "label match",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "label", Op: "equals", Value: "xagent"}}},
			event: InputEvent{Attrs: Attrs{"label": {"xagent", "urgent"}}},
			want:  true,
		},
		{
			name:  "label no match",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "label", Op: "equals", Value: "xagent"}}},
			event: InputEvent{Attrs: Attrs{"label": {"bug", "urgent"}}},
			want:  false,
		},
		{
			name:  "no label condition is a wildcard",
			rule:  RoutingRule{},
			event: InputEvent{Attrs: Attrs{"label": {"xagent"}}},
			want:  true,
		},
		{
			name:  "label condition with empty event labels no match",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "label", Op: "equals", Value: "xagent"}}},
			event: InputEvent{Attrs: Attrs{"label": nil}},
			want:  false,
		},
		{
			name:  "label equals requires exact membership not substring",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "label", Op: "equals", Value: "agent"}}},
			event: InputEvent{Attrs: Attrs{"label": {"xagent"}}},
			want:  false,
		},

		// --- section 2 semantics: AND, missing attr, multi-value, contains ---
		{
			name: "multiple conditions AND together (all match)",
			rule: RoutingRule{Conditions: []Condition{
				{Attr: "body", Op: "prefix", Value: "xagent:"},
				{Attr: "label", Op: "equals", Value: "urgent"},
			}},
			event: InputEvent{Data: "xagent: do it", Attrs: Attrs{"label": {"urgent"}}},
			want:  true,
		},
		{
			name: "multiple conditions AND together (one fails)",
			rule: RoutingRule{Conditions: []Condition{
				{Attr: "body", Op: "prefix", Value: "xagent:"},
				{Attr: "label", Op: "equals", Value: "urgent"},
			}},
			event: InputEvent{Data: "xagent: do it", Attrs: Attrs{"label": {"backlog"}}},
			want:  false,
		},
		{
			name: "condition on a missing attr fails even when others match",
			rule: RoutingRule{Conditions: []Condition{
				{Attr: "body", Op: "prefix", Value: "xagent:"},
				{Attr: "assignee", Op: "equals", Value: "icholy-bot"},
			}},
			event: InputEvent{Data: "xagent: do it"},
			want:  false,
		},
		{
			name:  "multi-value attr membership: any value matches",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "label", Op: "equals", Value: "urgent"}}},
			event: InputEvent{Attrs: Attrs{"label": {"backlog", "urgent", "triage"}}},
			want:  true,
		},
		{
			name:  "contains op matches a substring of a value",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "body", Op: "contains", Value: "please"}}},
			event: InputEvent{Data: "hey, please fix this"},
			want:  true,
		},
		{
			name:  "contains op mismatch",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "body", Op: "contains", Value: "please"}}},
			event: InputEvent{Data: "just a comment"},
			want:  false,
		},

		// --- combined selector + conditions ---
		{
			name: "combined source type label all match",
			rule: RoutingRule{
				Source:     "atlassian",
				Type:       "label_added",
				Conditions: []Condition{{Attr: "label", Op: "equals", Value: "xagent"}},
			},
			event: InputEvent{
				Source: "atlassian",
				Type:   "label_added",
				Attrs:  Attrs{"label": {"xagent"}},
			},
			want: true,
		},
		{
			name: "combined label mismatch",
			rule: RoutingRule{
				Source:     "atlassian",
				Type:       "label_added",
				Conditions: []Condition{{Attr: "label", Op: "equals", Value: "xagent"}},
			},
			event: InputEvent{
				Source: "atlassian",
				Type:   "label_added",
				Attrs:  Attrs{"label": {"bug"}},
			},
			want: false,
		},
		{
			name: "combined gate type mismatch short-circuits conditions",
			rule: RoutingRule{
				Source:     "github",
				Type:       "pull_request_assigned",
				Conditions: []Condition{{Attr: "assignee", Op: "equals", Value: "icholy-bot"}},
			},
			event: InputEvent{
				Source: "github",
				Type:   "issue_assigned",
				Attrs:  Attrs{"assignee": {"icholy-bot"}},
			},
			want: false,
		},
		{
			name: "combined gate assignee condition mismatch",
			rule: RoutingRule{
				Source:     "github",
				Type:       "pull_request_assigned",
				Conditions: []Condition{{Attr: "assignee", Op: "equals", Value: "icholy-bot"}},
			},
			event: InputEvent{
				Source: "github",
				Type:   "pull_request_assigned",
				Attrs:  Attrs{"assignee": {"octocat"}},
			},
			want: false,
		},

		// --- unknown op never matches ---
		{
			name:  "unknown op never matches",
			rule:  RoutingRule{Conditions: []Condition{{Attr: "body", Op: "regex", Value: "xagent:"}}},
			event: InputEvent{Data: "xagent: do it"},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, MatchRule(tt.rule, tt.event), tt.want)
		})
	}
}
