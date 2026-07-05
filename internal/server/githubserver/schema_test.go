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
	assert.DeepEqual(t, attrKeys(def), []string{"body", "url", "mention"})
	assert.Assert(t, len(def.DefaultRules) > 0, "issue_comment DefaultRules is empty; want the xagent: wakeup rule")

	// AttrDefs carry GitHub-flavoured display copy for the routing-rule editor.
	mention := def.Attrs[2]
	assert.Equal(t, mention.Key, "mention")
	assert.Equal(t, mention.Label, "Mention")
	assert.Equal(t, mention.Placeholder, "octocat")
	assert.Assert(t, mention.Help != "", "mention attr Help is empty; want GitHub mention help copy")

	// A non-comment type registers but ships no default rules.
	labelDef, ok := reg.EventTypeFor("github", EventTypeLabelAdded)
	assert.Assert(t, ok, "EventTypeFor(github, %q) = _, false; want hit", EventTypeLabelAdded)
	assert.Assert(t, labelDef.DefaultRules == nil, "label_added DefaultRules = %v, want nil", labelDef.DefaultRules)
}

// attrKeys extracts the attr keys from an event-type def, in order.
func attrKeys(def eventrouter.EventTypeDef) []string {
	keys := make([]string, len(def.Attrs))
	for i, a := range def.Attrs {
		keys[i] = a.Key
	}
	return keys
}
