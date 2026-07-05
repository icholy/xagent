package atlassianserver

import "github.com/icholy/xagent/internal/eventrouter2"

// init registers atlassianserver's schemas on the process-wide default registry.
func init() {
	registerSchemas(eventrouter2.DefaultSchemaRegistry)
}

// registerSchemas records the eventrouter2 schema for every event type
// atlassianserver emits (see toInputEvent) on reg. Each schema declares the
// complete valid attr set for its type — the derived body/url plus its emitted
// dimensions — and the default rules the producer ships. comment_created carries
// the "xagent:" body-prefix wakeup default; label_added ships no default rules.
func registerSchemas(reg *eventrouter2.SchemaRegistry) {
	reg.MustRegister(eventrouter2.EventTypeDef{
		Source: "atlassian",
		Type:   EventTypeCommentCreated,
		Label:  "Jira: Issue Comment",
		Attrs:  []string{"body", "url", "mention"},
		DefaultRules: []eventrouter2.RoutingRule{{
			Source:     "atlassian",
			Type:       EventTypeCommentCreated,
			Conditions: []eventrouter2.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
			Wakeup:     true,
		}},
	})
	reg.MustRegister(eventrouter2.EventTypeDef{
		Source: "atlassian",
		Type:   EventTypeLabelAdded,
		Label:  "Jira: Label Added",
		Attrs:  []string{"body", "url", "label"},
	})
}
