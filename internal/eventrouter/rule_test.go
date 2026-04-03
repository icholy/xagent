package eventrouter

import (
	"testing"

	"github.com/icholy/xagent/internal/model"
	"gotest.tools/v3/assert"
)

func TestRuleMarshalUnmarshal(t *testing.T) {
	rules := []Rule{
		{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
		{Mention: "botuser"},
		{},
	}
	data, err := model.MarshalRoutingRules(rules)
	assert.NilError(t, err)

	got, err := model.UnmarshalRoutingRules(data)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, rules)
}

func TestUnmarshalRulesEmpty(t *testing.T) {
	rules, err := model.UnmarshalRoutingRules(nil)
	assert.NilError(t, err)
	assert.Assert(t, rules == nil)

	rules, err = model.UnmarshalRoutingRules([]byte("[]"))
	assert.NilError(t, err)
	assert.Equal(t, len(rules), 0)
}

func TestRuleMatch(t *testing.T) {
	tests := []struct {
		name   string
		rule   Rule
		source string
		typ    string
		data   string
		want   bool
	}{
		{
			name:   "empty rule matches everything",
			rule:   Rule{},
			source: "github", typ: "issue_comment", data: "hello",
			want: true,
		},
		{
			name:   "source match",
			rule:   Rule{Source: "github"},
			source: "github",
			want:   true,
		},
		{
			name:   "source mismatch",
			rule:   Rule{Source: "github"},
			source: "atlassian",
			want:   false,
		},
		{
			name:   "type match",
			rule:   Rule{Type: "issue_comment"},
			source: "github", typ: "issue_comment",
			want: true,
		},
		{
			name:   "type mismatch",
			rule:   Rule{Type: "issue_comment"},
			source: "github", typ: "pull_request_review",
			want: false,
		},
		{
			name: "prefix match",
			rule: Rule{Prefix: "xagent:"},
			data: "xagent: fix the tests",
			want: true,
		},
		{
			name: "prefix mismatch",
			rule: Rule{Prefix: "xagent:"},
			data: "just a comment",
			want: false,
		},
		{
			name:   "github mention match",
			rule:   Rule{Mention: "botuser"},
			source: "github", data: "hey @botuser fix this",
			want: true,
		},
		{
			name:   "github mention case insensitive",
			rule:   Rule{Mention: "BotUser"},
			source: "github", data: "hey @botuser fix this",
			want: true,
		},
		{
			name:   "github mention no substring match",
			rule:   Rule{Mention: "bot"},
			source: "github", data: "hey @botuser fix this",
			want: false,
		},
		{
			name:   "github mention at start",
			rule:   Rule{Mention: "botuser"},
			source: "github", data: "@botuser fix this",
			want: true,
		},
		{
			name:   "github mention mismatch",
			rule:   Rule{Mention: "botuser"},
			source: "github", data: "no mention here",
			want: false,
		},
		{
			name:   "atlassian mention match",
			rule:   Rule{Mention: "abc123"},
			source: "atlassian", data: "hey [~accountid:abc123] fix this",
			want: true,
		},
		{
			name:   "atlassian mention mismatch",
			rule:   Rule{Mention: "abc123"},
			source: "atlassian", data: "no mention here",
			want: false,
		},
		{
			name:   "unknown source mention never matches",
			rule:   Rule{Mention: "user"},
			source: "unknown", data: "@user hello",
			want: false,
		},
		{
			name:   "all fields must match",
			rule:   Rule{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
			source: "github", typ: "issue_comment", data: "xagent: do it",
			want: true,
		},
		{
			name:   "all fields source mismatch",
			rule:   Rule{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
			source: "atlassian", typ: "issue_comment", data: "xagent: do it",
			want: false,
		},
		{
			name:   "all fields prefix mismatch",
			rule:   Rule{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
			source: "github", typ: "issue_comment", data: "just a comment",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.rule.Match(tt.source, tt.typ, tt.data), tt.want)
		})
	}
}
