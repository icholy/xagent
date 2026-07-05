package atlassianserver

import (
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
)

// init registers atlassianserver's schemas on the process-wide default registry.
func init() {
	RegisterSchemas(eventrouter.DefaultSchemaRegistry)
}

// RegisterSchemas records the eventrouter schema for every event type
// atlassianserver emits (see toInputEvent) on reg. Each schema declares the
// complete valid attr set for its type — the derived body/url plus its emitted
// dimensions — and the default rules the producer ships. comment_created carries
// the "xagent:" body-prefix wakeup default; label_added ships no default rules.
// It is exported so tests (e.g. eventrouter's Route tests) can populate an
// isolated registry with the real producer schemas.
func RegisterSchemas(reg *eventrouter.SchemaRegistry) {
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "atlassian",
		Type:   EventTypeCommentCreated,
		Label:  "Jira: Issue Comment",
		Attrs:  []string{"body", "url", "mention"},
		DefaultRules: []model.RoutingRule{{
			Source:     "atlassian",
			Type:       EventTypeCommentCreated,
			Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
			Wakeup:     true,
		}},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "atlassian",
		Type:   EventTypeLabelAdded,
		Label:  "Jira: Label Added",
		Attrs:  []string{"body", "url", "label"},
	})
}
