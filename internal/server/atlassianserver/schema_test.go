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
	assert.DeepEqual(t, attrKeys(def), []string{"body", "url", "mention"})
	assert.Assert(t, len(def.DefaultRules) > 0, "comment_created DefaultRules is empty; want the xagent: wakeup rule")

	// AttrDefs carry Jira-flavoured display copy for the routing-rule editor —
	// the mention placeholder is an Atlassian account id, not a GitHub username.
	mention := def.Attrs[2]
	assert.Equal(t, mention.Key, "mention")
	assert.Equal(t, mention.Label, "Mention")
	assert.Equal(t, mention.Placeholder, "5b10ac8d82e05b22cc7d4ef5")
	assert.Assert(t, mention.Help != "", "mention attr Help is empty; want Jira mention help copy")

	// label_added registers but ships no default rules.
	labelDef, ok := reg.EventTypeFor("atlassian", EventTypeLabelAdded)
	assert.Assert(t, ok, "EventTypeFor(atlassian, %q) = _, false; want hit", EventTypeLabelAdded)
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
