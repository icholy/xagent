package eventrouter

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestRuleMarshalUnmarshal(t *testing.T) {
	rules := []Rule{
		{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
		{Mention: "botuser"},
		{},
	}
	data, err := MarshalRules(rules)
	assert.NilError(t, err)

	got, err := UnmarshalRules(data)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, rules)
}

func TestUnmarshalRulesEmpty(t *testing.T) {
	rules, err := UnmarshalRules(nil)
	assert.NilError(t, err)
	assert.Assert(t, rules == nil)

	rules, err = UnmarshalRules([]byte("[]"))
	assert.NilError(t, err)
	assert.Equal(t, len(rules), 0)
}

func TestRuleMatch(t *testing.T) {
	tests := []struct {
		name  string
		rule  Rule
		event InputEvent
		want  bool
	}{
		{
			name:  "empty rule matches everything",
			rule:  Rule{},
			event: InputEvent{Source: "github", Type: "issue_comment", Data: "hello"},
			want:  true,
		},
		{
			name:  "source match",
			rule:  Rule{Source: "github"},
			event: InputEvent{Source: "github"},
			want:  true,
		},
		{
			name:  "source mismatch",
			rule:  Rule{Source: "github"},
			event: InputEvent{Source: "atlassian"},
			want:  false,
		},
		{
			name:  "type match",
			rule:  Rule{Type: "issue_comment"},
			event: InputEvent{Source: "github", Type: "issue_comment"},
			want:  true,
		},
		{
			name:  "type mismatch",
			rule:  Rule{Type: "issue_comment"},
			event: InputEvent{Source: "github", Type: "pull_request_review"},
			want:  false,
		},
		{
			name:  "prefix match",
			rule:  Rule{Prefix: "xagent:"},
			event: InputEvent{Data: "xagent: fix the tests"},
			want:  true,
		},
		{
			name:  "prefix mismatch",
			rule:  Rule{Prefix: "xagent:"},
			event: InputEvent{Data: "just a comment"},
			want:  false,
		},
		{
			name:  "github mention match",
			rule:  Rule{Mention: "botuser"},
			event: InputEvent{Source: "github", Data: "hey @botuser fix this"},
			want:  true,
		},
		{
			name:  "github mention case insensitive",
			rule:  Rule{Mention: "BotUser"},
			event: InputEvent{Source: "github", Data: "hey @botuser fix this"},
			want:  true,
		},
		{
			name:  "github mention no substring match",
			rule:  Rule{Mention: "bot"},
			event: InputEvent{Source: "github", Data: "hey @botuser fix this"},
			want:  false,
		},
		{
			name:  "github mention at start",
			rule:  Rule{Mention: "botuser"},
			event: InputEvent{Source: "github", Data: "@botuser fix this"},
			want:  true,
		},
		{
			name:  "github mention mismatch",
			rule:  Rule{Mention: "botuser"},
			event: InputEvent{Source: "github", Data: "no mention here"},
			want:  false,
		},
		{
			name:  "atlassian mention match",
			rule:  Rule{Mention: "abc123"},
			event: InputEvent{Source: "atlassian", Data: "hey [~accountid:abc123] fix this"},
			want:  true,
		},
		{
			name:  "atlassian mention mismatch",
			rule:  Rule{Mention: "abc123"},
			event: InputEvent{Source: "atlassian", Data: "no mention here"},
			want:  false,
		},
		{
			name:  "unknown source mention never matches",
			rule:  Rule{Mention: "user"},
			event: InputEvent{Source: "unknown", Data: "@user hello"},
			want:  false,
		},
		{
			name:  "all fields must match",
			rule:  Rule{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
			event: InputEvent{Source: "github", Type: "issue_comment", Data: "xagent: do it"},
			want:  true,
		},
		{
			name:  "all fields source mismatch",
			rule:  Rule{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
			event: InputEvent{Source: "atlassian", Type: "issue_comment", Data: "xagent: do it"},
			want:  false,
		},
		{
			name:  "all fields prefix mismatch",
			rule:  Rule{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
			event: InputEvent{Source: "github", Type: "issue_comment", Data: "just a comment"},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.rule.Match(tt.event), tt.want)
		})
	}
}
