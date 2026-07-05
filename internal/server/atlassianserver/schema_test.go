package atlassianserver

import (
	"testing"

	"github.com/icholy/xagent/internal/eventrouter2"
	"gotest.tools/v3/assert"
)

func TestSchemaRegistration(t *testing.T) {
	reg := eventrouter2.NewSchemaRegistry()
	RegisterSchemas(reg)

	def, ok := reg.EventTypeFor("atlassian", EventTypeCommentCreated)
	assert.Assert(t, ok, "EventTypeFor(atlassian, %q) = _, false; want hit", EventTypeCommentCreated)
	assert.DeepEqual(t, def.Attrs, []string{"body", "url", "mention"})
	assert.Assert(t, len(def.DefaultRules) > 0, "comment_created DefaultRules is empty; want the xagent: wakeup rule")

	// label_added registers but ships no default rules.
	labelDef, ok := reg.EventTypeFor("atlassian", EventTypeLabelAdded)
	assert.Assert(t, ok, "EventTypeFor(atlassian, %q) = _, false; want hit", EventTypeLabelAdded)
	assert.Assert(t, labelDef.DefaultRules == nil, "label_added DefaultRules = %v, want nil", labelDef.DefaultRules)
}
