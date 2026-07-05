package githubserver

import (
	"testing"

	"github.com/icholy/xagent/internal/eventrouter2"
	"gotest.tools/v3/assert"
)

func TestSchemaRegistration(t *testing.T) {
	reg := eventrouter2.NewSchemaRegistry()
	registerSchemas(reg)

	def, ok := reg.EventTypeFor("github", EventTypeIssueComment)
	assert.Assert(t, ok, "EventTypeFor(github, %q) = _, false; want hit", EventTypeIssueComment)
	assert.DeepEqual(t, def.Attrs, []string{"body", "url", "mention"})
	assert.Assert(t, len(def.DefaultRules) > 0, "issue_comment DefaultRules is empty; want the xagent: wakeup rule")

	// A non-comment type registers but ships no default rules.
	labelDef, ok := reg.EventTypeFor("github", EventTypeLabelAdded)
	assert.Assert(t, ok, "EventTypeFor(github, %q) = _, false; want hit", EventTypeLabelAdded)
	assert.Assert(t, labelDef.DefaultRules == nil, "label_added DefaultRules = %v, want nil", labelDef.DefaultRules)
}
