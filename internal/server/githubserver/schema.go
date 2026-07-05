package githubserver

import (
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
)

// wakeOnXagentPrefix is the default rule shipped for GitHub comment/review event
// types: wake the linked task when the comment body is prefixed with "xagent:".
func wakeOnXagentPrefix(typ string) model.RoutingRule {
	return model.RoutingRule{
		Source:     "github",
		Type:       typ,
		Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
		Wakeup:     true,
	}
}

// Shared AttrDefs composed into githubserver's event-type schemas. Each carries
// the display copy (label/help/placeholder) the routing-rule editor renders, so
// the copy lives with the schema rather than being hardcoded in the frontend.
// body and url are the universal dimensions derived from every event; mention,
// assignee, label, and state are GitHub-specific. The mention and url copy is
// GitHub-flavoured (a username, a github.com URL), differing from the Atlassian
// producer's variants.
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
		Placeholder: "https://github.com/owner/repo/",
		Help:        "Matched against the event URL — e.g. to scope a rule to a single repo or project.",
	}
	attrMention = eventrouter.AttrDef{
		Key:         "mention",
		Label:       "Mention",
		Placeholder: "octocat",
		Help:        "GitHub username mentioned in the event body (no leading @).",
	}
	attrAssignee = eventrouter.AttrDef{
		Key:         "assignee",
		Label:       "Assignee",
		Placeholder: "icholy-bot",
		Help:        "The new assignee on an assignment event (GitHub username, no leading @).",
	}
	attrLabel = eventrouter.AttrDef{
		Key:         "label",
		Label:       "Label",
		Placeholder: "xagent",
		Help:        "A label added to the issue or PR.",
	}
	attrState = eventrouter.AttrDef{
		Key:         "state",
		Label:       "State",
		Placeholder: "merged",
		Help:        `The resulting state — e.g. "merged" or "closed" for a closed PR.`,
	}
)

// init registers githubserver's schemas on the process-wide default registry.
func init() {
	RegisterSchemas(eventrouter.DefaultSchemaRegistry)
}

// RegisterSchemas records the eventrouter schema for every event type
// githubserver emits (see toInputEvent) on reg. Each schema declares the
// complete valid attr set for its type — the derived body/url plus its emitted
// dimensions — and the default rules the producer ships. Only the three
// comment/review types carry the "xagent:" body-prefix wakeup default; the rest
// ship no default rules. It is exported so tests (e.g. eventrouter's Route
// tests) can populate an isolated registry with the real producer schemas.
func RegisterSchemas(reg *eventrouter.SchemaRegistry) {
	reg.MustRegister(eventrouter.EventTypeDef{
		Source:       "github",
		Type:         EventTypeIssueComment,
		Label:        "GitHub: Issue/PR Comment",
		Attrs:        []eventrouter.AttrDef{attrBody, attrURL, attrMention},
		DefaultRules: []model.RoutingRule{wakeOnXagentPrefix(EventTypeIssueComment)},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source:       "github",
		Type:         EventTypePullRequestReviewComment,
		Label:        "GitHub: PR Review Comment",
		Attrs:        []eventrouter.AttrDef{attrBody, attrURL, attrMention},
		DefaultRules: []model.RoutingRule{wakeOnXagentPrefix(EventTypePullRequestReviewComment)},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source:       "github",
		Type:         EventTypePullRequestReview,
		Label:        "GitHub: PR Review",
		Attrs:        []eventrouter.AttrDef{attrBody, attrURL, attrMention},
		DefaultRules: []model.RoutingRule{wakeOnXagentPrefix(EventTypePullRequestReview)},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "github",
		Type:   EventTypeIssueAssigned,
		Label:  "GitHub: Issue Assigned",
		Attrs:  []eventrouter.AttrDef{attrBody, attrURL, attrAssignee},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "github",
		Type:   EventTypePullRequestAssigned,
		Label:  "GitHub: PR Assigned",
		Attrs:  []eventrouter.AttrDef{attrBody, attrURL, attrAssignee},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "github",
		Type:   EventTypePullRequestOpened,
		Label:  "GitHub: PR Opened",
		Attrs:  []eventrouter.AttrDef{attrBody, attrURL},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "github",
		Type:   EventTypePullRequestClosed,
		Label:  "GitHub: PR Closed",
		Attrs:  []eventrouter.AttrDef{attrBody, attrURL, attrState},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "github",
		Type:   EventTypeLabelAdded,
		Label:  "GitHub: Label Added",
		Attrs:  []eventrouter.AttrDef{attrBody, attrURL, attrLabel},
	})
}
