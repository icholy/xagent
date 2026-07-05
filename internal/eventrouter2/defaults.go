package eventrouter2

// DefaultRules is the new-shape replacement for the legacy type-less
// {Prefix:"xagent:", Wakeup:true} fallback. Rather than a validation
// special-case, the default set is an ordinary list of fully-defined rules: one
// per comment/review event type, each waking on a body prefixed with "xagent:".
// Every entry passes Validate (guarded by a test). These are not wired into the
// router here; that is a later layer.
var DefaultRules = []RoutingRule{
	{
		Source:     "github",
		Type:       "issue_comment",
		Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
		Wakeup:     true,
	},
	{
		Source:     "github",
		Type:       "pull_request_review_comment",
		Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
		Wakeup:     true,
	},
	{
		Source:     "github",
		Type:       "pull_request_review",
		Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
		Wakeup:     true,
	},
	{
		Source:     "atlassian",
		Type:       "comment_created",
		Conditions: []Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
		Wakeup:     true,
	},
}
