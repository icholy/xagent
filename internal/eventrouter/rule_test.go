package eventrouter

import (
	"testing"

	"github.com/icholy/xagent/internal/model"
	"gotest.tools/v3/assert"
)

func TestMatchRule(t *testing.T) {
	tests := []struct {
		name  string
		rule  model.RoutingRule
		event InputEvent
		want  bool
	}{
		{
			name:  "empty rule matches everything",
			rule:  model.RoutingRule{},
			event: InputEvent{Source: "github", Type: "issue_comment", Data: "hello"},
			want:  true,
		},
		{
			name:  "source match",
			rule:  model.RoutingRule{Source: "github"},
			event: InputEvent{Source: "github"},
			want:  true,
		},
		{
			name:  "source mismatch",
			rule:  model.RoutingRule{Source: "github"},
			event: InputEvent{Source: "atlassian"},
			want:  false,
		},
		{
			name:  "type match",
			rule:  model.RoutingRule{Type: "issue_comment"},
			event: InputEvent{Source: "github", Type: "issue_comment"},
			want:  true,
		},
		{
			name:  "type mismatch",
			rule:  model.RoutingRule{Type: "issue_comment"},
			event: InputEvent{Source: "github", Type: "pull_request_review"},
			want:  false,
		},
		{
			name:  "prefix match",
			rule:  model.RoutingRule{Prefix: "xagent:"},
			event: InputEvent{Data: "xagent: fix the tests"},
			want:  true,
		},
		{
			name:  "prefix mismatch",
			rule:  model.RoutingRule{Prefix: "xagent:"},
			event: InputEvent{Data: "just a comment"},
			want:  false,
		},
		{
			name:  "github mention match",
			rule:  model.RoutingRule{Mention: "botuser"},
			event: InputEvent{Source: "github", Data: "hey @botuser fix this"},
			want:  true,
		},
		{
			name:  "github mention case insensitive",
			rule:  model.RoutingRule{Mention: "BotUser"},
			event: InputEvent{Source: "github", Data: "hey @botuser fix this"},
			want:  true,
		},
		{
			name:  "github mention no substring match",
			rule:  model.RoutingRule{Mention: "bot"},
			event: InputEvent{Source: "github", Data: "hey @botuser fix this"},
			want:  false,
		},
		{
			name:  "github mention at start",
			rule:  model.RoutingRule{Mention: "botuser"},
			event: InputEvent{Source: "github", Data: "@botuser fix this"},
			want:  true,
		},
		{
			name:  "github mention mismatch",
			rule:  model.RoutingRule{Mention: "botuser"},
			event: InputEvent{Source: "github", Data: "no mention here"},
			want:  false,
		},
		{
			name:  "atlassian mention match",
			rule:  model.RoutingRule{Mention: "abc123"},
			event: InputEvent{Source: "atlassian", Data: "hey [~accountid:abc123] fix this"},
			want:  true,
		},
		{
			name:  "atlassian mention mismatch",
			rule:  model.RoutingRule{Mention: "abc123"},
			event: InputEvent{Source: "atlassian", Data: "no mention here"},
			want:  false,
		},
		{
			name:  "unknown source mention never matches",
			rule:  model.RoutingRule{Mention: "user"},
			event: InputEvent{Source: "unknown", Data: "@user hello"},
			want:  false,
		},
		{
			name:  "all fields must match",
			rule:  model.RoutingRule{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
			event: InputEvent{Source: "github", Type: "issue_comment", Data: "xagent: do it"},
			want:  true,
		},
		{
			name:  "all fields source mismatch",
			rule:  model.RoutingRule{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
			event: InputEvent{Source: "atlassian", Type: "issue_comment", Data: "xagent: do it"},
			want:  false,
		},
		{
			name:  "all fields prefix mismatch",
			rule:  model.RoutingRule{Source: "github", Type: "issue_comment", Prefix: "xagent:"},
			event: InputEvent{Source: "github", Type: "issue_comment", Data: "just a comment"},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.event.MatchRule(tt.rule), tt.want)
		})
	}
}
