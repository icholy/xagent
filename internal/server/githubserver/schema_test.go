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

	// AttrDefs carry GitHub-flavoured display copy declared inline per event
	// type, so the copy speaks to this specific event (a comment's body/URL).
	assert.DeepEqual(t, def.Attrs, []eventrouter.AttrDef{
		{
			Key:         "body",
			Label:       "Comment Body",
			Placeholder: "xagent:",
			Help:        "Matched against the comment text.",
		},
		{
			Key:         "url",
			Label:       "Issue/PR URL",
			Placeholder: "https://github.com/owner/repo/",
			Help:        "Matched against the commented issue or PR URL, e.g. to scope a rule to a single repo.",
		},
		{
			Key:         "mention",
			Label:       "Mention",
			Placeholder: "octocat",
			Help:        "GitHub username @-mentioned in the comment (no leading @).",
		},
		{
			Key:         "user",
			Label:       "User",
			Placeholder: "octocat",
			Help:        "The GitHub username of the user who commented (no leading @).",
		},
	})

	// A non-comment type registers but ships no default rules.
	labelDef, ok := reg.EventTypeFor("github", EventTypeLabelAdded)
	assert.Assert(t, ok, "EventTypeFor(github, %q) = _, false; want hit", EventTypeLabelAdded)
	assert.Assert(t, labelDef.DefaultRules == nil, "label_added DefaultRules = %v, want nil", labelDef.DefaultRules)
}
