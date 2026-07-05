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

	// AttrDefs carry Jira-flavoured display copy for the routing-rule editor —
	// the mention placeholder is an Atlassian account id, not a GitHub username.
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
			Placeholder: "https://your-domain.atlassian.net/browse/PROJ-",
			Help:        "Matched against the event URL — e.g. to scope a rule to a single repo or project.",
		},
		{
			Key:         "mention",
			Label:       "Mention",
			Placeholder: "5b10ac8d82e05b22cc7d4ef5",
			Help:        "Atlassian account id mentioned in the event body. Enter the bare id (no [~accountid:…] wrapper).",
		},
	})

	// label_added registers but ships no default rules.
	labelDef, ok := reg.EventTypeFor("atlassian", EventTypeLabelAdded)
	assert.Assert(t, ok, "EventTypeFor(atlassian, %q) = _, false; want hit", EventTypeLabelAdded)
	assert.Assert(t, labelDef.DefaultRules == nil, "label_added DefaultRules = %v, want nil", labelDef.DefaultRules)
}
