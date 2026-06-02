package githubserver

import (
	"context"

	"github.com/shurcooL/githubv4"

	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/x/githubx"
)

// react adds a reaction to the resource that triggered the outcome.
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
	content, ok := reactionContent(outcome)
	if !ok {
		return nil
	}
	org, err := s.store.GetOrg(ctx, nil, outcome.OrgID)
	if err != nil {
		return err
	}
	if org.GitHubInstallationID == 0 {
		return nil
	}

	// The GraphQL client is authenticated as the App's bot identity, backed by
	// the cached auto-refreshing installation transport so the reaction is
	// attributed to the App and repeated calls skip re-minting the token.
	client := githubv4.NewClient(s.tokens.Client(org.GitHubInstallationID))
	return githubx.AddReaction(ctx, client, meta.NodeID, content)
}

// reactionContent picks the emoji for a routing outcome, returning ok=false when
// no reaction should be added:
//
//	created task                  -> 🚀
//	woken/attached task(s)        -> 👀
//	matched-only, waking rule     -> 😕
//	matched-only, non-waking rule -> no reaction
//
// The matched-only 😕 flags a rule that expected to act but found no task to
// wake. A rule with Wakeup: false (and no create action) is passive by design —
// doing nothing for an unlinked resource is its intended behavior, not a
// misconfiguration — so reacting 😕 to every such event (e.g. every closed PR
// in the repo) would be noise. Stay silent in that case.
func reactionContent(outcome eventrouter.RouteOutcome) (githubv4.ReactionContent, bool) {
	switch {
	case outcome.Created:
		return githubv4.ReactionContentRocket, true
	case len(outcome.TaskIDs) > 0:
		return githubv4.ReactionContentEyes, true
	case outcome.Rule != nil && outcome.Rule.Wakeup:
		return githubv4.ReactionContentConfused, true
	default:
		return "", false
	}
}
