package atlassianserver

import (
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
)

// Shared AttrDefs composed into atlassianserver's event-type schemas. Each
// carries the display copy (label/help/placeholder) the routing-rule editor
// renders, so the copy lives with the schema rather than being hardcoded in the
// frontend. body is the universal dimension derived from every event; url,
// mention, and label are Jira-flavoured — the url and mention copy differs from
// the GitHub producer's variants (a Jira browse URL, an Atlassian account id).
var (
	attrBody = eventrouter.AttrDef{
		Key:         "body",
		Label:       "Body",
		Placeholder: "xagent:",
		Help:        "Matched against the event body — the comment or description text.",
	}
	attrURL = eventrouter.AttrDef{
		Key:         "url",
		Label:       "URL",
		Placeholder: "https://your-domain.atlassian.net/browse/PROJ-",
		Help:        "Matched against the event URL — e.g. to scope a rule to a single repo or project.",
	}
	attrMention = eventrouter.AttrDef{
		Key:         "mention",
		Label:       "Mention",
		Placeholder: "5b10ac8d82e05b22cc7d4ef5",
		Help:        "Atlassian account id mentioned in the event body. Enter the bare id (no [~accountid:…] wrapper).",
	}
	attrLabel = eventrouter.AttrDef{
		Key:         "label",
		Label:       "Label",
		Placeholder: "xagent",
		Help:        "A label added to the issue or PR.",
	}
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
		Attrs:  []eventrouter.AttrDef{attrBody, attrURL, attrMention},
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
		Attrs:  []eventrouter.AttrDef{attrBody, attrURL, attrLabel},
	})
}
