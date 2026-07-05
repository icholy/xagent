package githubx

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestMentions(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "none", text: "just a plain comment", want: nil},
		{name: "single", text: "hey @icholy-bot please look", want: []string{"icholy-bot"}},
		{name: "at start", text: "@alice ping", want: []string{"alice"}},
		{name: "case insensitive prefix", text: "cc @Bob", want: []string{"Bob"}},
		{
			name: "adjacent and punctuation",
			text: "(@alice) @bob, cc @carol!",
			want: []string{"alice", "bob", "carol"},
		},
		{
			name: "team reference excluded",
			text: "see @alice/team for details",
			want: nil,
		},
		{
			name: "email-like not a mention",
			text: "contact foo@example.com",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, Mentions(tt.text), tt.want)
		})
	}
}
