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
		Attrs:        []string{"body", "url", "mention"},
		DefaultRules: []model.RoutingRule{wakeOnXagentPrefix(EventTypeIssueComment)},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source:       "github",
		Type:         EventTypePullRequestReviewComment,
		Label:        "GitHub: PR Review Comment",
		Attrs:        []string{"body", "url", "mention"},
		DefaultRules: []model.RoutingRule{wakeOnXagentPrefix(EventTypePullRequestReviewComment)},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source:       "github",
		Type:         EventTypePullRequestReview,
		Label:        "GitHub: PR Review",
		Attrs:        []string{"body", "url", "mention"},
		DefaultRules: []model.RoutingRule{wakeOnXagentPrefix(EventTypePullRequestReview)},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "github",
		Type:   EventTypeIssueAssigned,
		Label:  "GitHub: Issue Assigned",
		Attrs:  []string{"body", "url", "assignee"},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "github",
		Type:   EventTypePullRequestAssigned,
		Label:  "GitHub: PR Assigned",
		Attrs:  []string{"body", "url", "assignee"},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "github",
		Type:   EventTypePullRequestOpened,
		Label:  "GitHub: PR Opened",
		Attrs:  []string{"body", "url"},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "github",
		Type:   EventTypePullRequestClosed,
		Label:  "GitHub: PR Closed",
		Attrs:  []string{"body", "url", "state"},
	})
	reg.MustRegister(eventrouter.EventTypeDef{
		Source: "github",
		Type:   EventTypeLabelAdded,
		Label:  "GitHub: Label Added",
		Attrs:  []string{"body", "url", "label"},
	})
}
