package githubserver

import "github.com/icholy/xagent/internal/eventrouter2"

// wakeOnXagentPrefix is the default rule shipped for GitHub comment/review event
// types: wake the linked task when the comment body is prefixed with "xagent:".
func wakeOnXagentPrefix(typ string) eventrouter2.RoutingRule {
	return eventrouter2.RoutingRule{
		Source:     "github",
		Type:       typ,
		Conditions: []eventrouter2.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
		Wakeup:     true,
	}
}

// init registers the eventrouter2 schema for every event type githubserver emits
// (see toInputEvent). Each schema declares the complete valid attr set for its
// type — the derived body/url plus its emitted dimensions — and the default
// rules the producer ships. Only the three comment/review types carry the
// "xagent:" body-prefix wakeup default; the rest ship no default rules.
func init() {
	eventrouter2.MustRegisterSchema(eventrouter2.EventTypeDef{
		Source:       "github",
		Type:         EventTypeIssueComment,
		Label:        "GitHub: Issue/PR Comment",
		Attrs:        []string{"body", "url", "mention"},
		DefaultRules: []eventrouter2.RoutingRule{wakeOnXagentPrefix(EventTypeIssueComment)},
	})
	eventrouter2.MustRegisterSchema(eventrouter2.EventTypeDef{
		Source:       "github",
		Type:         EventTypePullRequestReviewComment,
		Label:        "GitHub: PR Review Comment",
		Attrs:        []string{"body", "url", "mention"},
		DefaultRules: []eventrouter2.RoutingRule{wakeOnXagentPrefix(EventTypePullRequestReviewComment)},
	})
	eventrouter2.MustRegisterSchema(eventrouter2.EventTypeDef{
		Source:       "github",
		Type:         EventTypePullRequestReview,
		Label:        "GitHub: PR Review",
		Attrs:        []string{"body", "url", "mention"},
		DefaultRules: []eventrouter2.RoutingRule{wakeOnXagentPrefix(EventTypePullRequestReview)},
	})
	eventrouter2.MustRegisterSchema(eventrouter2.EventTypeDef{
		Source: "github",
		Type:   EventTypeIssueAssigned,
		Label:  "GitHub: Issue Assigned",
		Attrs:  []string{"body", "url", "assignee"},
	})
	eventrouter2.MustRegisterSchema(eventrouter2.EventTypeDef{
		Source: "github",
		Type:   EventTypePullRequestAssigned,
		Label:  "GitHub: PR Assigned",
		Attrs:  []string{"body", "url", "assignee"},
	})
	eventrouter2.MustRegisterSchema(eventrouter2.EventTypeDef{
		Source: "github",
		Type:   EventTypePullRequestOpened,
		Label:  "GitHub: PR Opened",
		Attrs:  []string{"body", "url"},
	})
	eventrouter2.MustRegisterSchema(eventrouter2.EventTypeDef{
		Source: "github",
		Type:   EventTypePullRequestClosed,
		Label:  "GitHub: PR Closed",
		Attrs:  []string{"body", "url", "state"},
	})
	eventrouter2.MustRegisterSchema(eventrouter2.EventTypeDef{
		Source: "github",
		Type:   EventTypeLabelAdded,
		Label:  "GitHub: Label Added",
		Attrs:  []string{"body", "url", "label"},
	})
}
