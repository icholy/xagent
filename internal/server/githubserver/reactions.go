package githubserver

import (
	"context"

	"github.com/shurcooL/githubv4"

	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/x/githubx"
)

// react adds a reaction to the resource that triggered the outcome. It is a
// plain synchronous function: it does the work and returns an error. It owns no
// concurrency or lifetime policy — the WebhookHandler glue runs it in a
// goroutine and logs the error.
//
// Every reactable GitHub event carries the global node ID of its reactable
// target in GitHubMeta.NodeID — the comment for comment events, the review
// summary for review submissions, the issue/PR for assignments — and the
// reaction goes out over the GraphQL addReaction mutation, which accepts any of
// them uniformly. Returns nil (not an error) when there's nothing to do: a
// non-GitHub Meta, an event with no reactable target, or an org with no
// installation.
func (s *Server) react(ctx context.Context, outcome eventrouter.RouteOutcome) error {
	meta, ok := outcome.Input.Meta.(GitHubMeta)
	if !ok || meta.NodeID == "" {
		return nil
	}
	org, err := s.store.GetOrg(ctx, nil, outcome.OrgID)
	if err != nil {
		return err
	}
	if org.GitHubInstallationID == 0 {
		return nil
	}

	// Pick the emoji from the routing outcome: a created task gets 🚀, a woken
	// task 👀, and an event that matched a rule but created or woke nothing 😕.
	var content githubv4.ReactionContent
	switch {
	case outcome.Created:
		content = githubv4.ReactionContentRocket
	case len(outcome.TaskIDs) > 0:
		content = githubv4.ReactionContentEyes
	default:
		content = githubv4.ReactionContentConfused
	}

	// The GraphQL client is authenticated as the App's bot identity, backed by
	// the cached auto-refreshing installation transport so the reaction is
	// attributed to the App and repeated calls skip re-minting the token.
	client := githubv4.NewClient(s.tokens.Client(org.GitHubInstallationID))
	return githubx.AddReaction(ctx, client, meta.NodeID, content)
}
