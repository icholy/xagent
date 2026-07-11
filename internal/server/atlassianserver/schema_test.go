package atlassianserver

import (
	"testing"

	"github.com/icholy/xagent/internal/eventrouter"
	"gotest.tools/v3/assert"
)

func TestSchemaRegistration(t *testing.T) {
	reg := eventrouter.NewSchemaRegistry()
	RegisterSchemas(reg)

	def, ok := reg.EventTypeFor("atlassian", EventTypeCommentCreated)
	assert.Assert(t, ok, "EventTypeFor(atlassian, %q) = _, false; want hit", EventTypeCommentCreated)
	assert.Assert(t, len(def.DefaultRules) > 0, "comment_created DefaultRules is empty; want the xagent: wakeup rule")

	// AttrDefs carry Jira-flavoured display copy declared inline per event type;
	// the mention placeholder is an Atlassian account id, not a GitHub username.
	assert.DeepEqual(t, def.Attrs, []eventrouter.AttrDef{
		{
			Key:         "body",
			Label:       "Comment Body",
			Placeholder: "xagent:",
			Help:        "Matched against the comment text.",
		},
		{
			Key:         "url",
			Label:       "Issue URL",
			Placeholder: "https://your-domain.atlassian.net/browse/PROJ-",
			Help:        "Matched against the commented issue URL, e.g. to scope a rule to a single project.",
		},
		{
			Key:         "mention",
			Label:       "Mention",
			Placeholder: "5b10ac8d82e05b22cc7d4ef5",
			Help:        "Atlassian account id @-mentioned in the comment. Enter the bare id (no [~accountid:…] wrapper).",
		},
		{
			Key:         "user",
			Label:       "User",
			Placeholder: "5b10ac8d82e05b22cc7d4ef5",
			Help:        "Atlassian account id of the user who commented. Enter the bare id (no [~accountid:…] wrapper).",
		},
	})

	// label_added registers but ships no default rules.
	labelDef, ok := reg.EventTypeFor("atlassian", EventTypeLabelAdded)
	assert.Assert(t, ok, "EventTypeFor(atlassian, %q) = _, false; want hit", EventTypeLabelAdded)
	assert.Assert(t, labelDef.DefaultRules == nil, "label_added DefaultRules = %v, want nil", labelDef.DefaultRules)
}
