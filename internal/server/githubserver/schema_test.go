package githubserver

import (
	"testing"

	"github.com/icholy/xagent/internal/eventrouter"
	"gotest.tools/v3/assert"
)

func TestSchemaRegistration(t *testing.T) {
	reg := eventrouter.NewSchemaRegistry()
	RegisterSchemas(reg)

	def, ok := reg.EventTypeFor("github", EventTypeIssueComment)
	assert.Assert(t, ok, "EventTypeFor(github, %q) = _, false; want hit", EventTypeIssueComment)
	assert.Assert(t, len(def.DefaultRules) > 0, "issue_comment DefaultRules is empty; want the xagent: wakeup rule")

	// AttrDefs carry GitHub-flavoured display copy for the routing-rule editor.
	assert.DeepEqual(t, def.Attrs, []eventrouter.AttrDef{
		{
			Key:         "body",
			Label:       "Body",
			Placeholder: "xagent:",
			Help:        "Matched against the event body — the comment or description text.",
		},
		{
			Key:         "url",
			Label:       "URL",
			Placeholder: "https://github.com/owner/repo/",
			Help:        "Matched against the event URL — e.g. to scope a rule to a single repo or project.",
		},
		{
			Key:         "mention",
			Label:       "Mention",
			Placeholder: "octocat",
			Help:        "GitHub username mentioned in the event body (no leading @).",
		},
	})

	// A non-comment type registers but ships no default rules.
	labelDef, ok := reg.EventTypeFor("github", EventTypeLabelAdded)
	assert.Assert(t, ok, "EventTypeFor(github, %q) = _, false; want hit", EventTypeLabelAdded)
	assert.Assert(t, labelDef.DefaultRules == nil, "label_added DefaultRules = %v, want nil", labelDef.DefaultRules)
}
