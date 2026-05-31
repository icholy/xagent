package githubserver

import (
	"context"

	"github.com/icholy/xagent/internal/eventrouter"
)

// react adds a reaction to the comment that triggered the outcome. It is a
// plain synchronous function: it does the work and returns an error. It owns no
// concurrency or lifetime policy — the WebhookHandler glue runs it in a
// goroutine and logs the error. Returns nil (not an error) when there's nothing
// to do: a non-GitHub Meta, an event with no reactable comment (CommentID == 0),
// an org with no installation, or a non-reactable event type.
func (s *Server) react(ctx context.Context, outcome eventrouter.RouteOutcome) error {
	meta, ok := outcome.Input.Meta.(GitHubMeta)
	if !ok || meta.CommentID == 0 {
		return nil
	}
	org, err := s.store.GetOrg(ctx, nil, outcome.OrgID)
	if err != nil {
		return err
	}
	if org.GitHubInstallationID == 0 {
		return nil
	}
	// Build a client authenticated as the App's bot identity, backed by the
	// cached auto-refreshing installation transport so the reaction is
	// attributed to the App and repeated calls skip re-minting the token.
	client, err := s.tokens.Client(org.GitHubInstallationID)
	if err != nil {
		return err
	}

	// Pick the emoji from the routing outcome: a created task gets 🚀, a woken
	// task 👀, and a comment that matched a rule but created or woke nothing 😕.
	var content string
	switch {
	case outcome.Created:
		content = "rocket"
	case len(outcome.TaskIDs) > 0:
		content = "eyes"
	default:
		content = "confused"
	}

	switch outcome.Input.Type {
	case EventTypeIssueComment:
		_, _, err = client.Reactions.CreateIssueCommentReaction(ctx, meta.Owner, meta.Repo, meta.CommentID, content)
	case EventTypePullRequestReviewComment:
		_, _, err = client.Reactions.CreatePullRequestCommentReaction(ctx, meta.Owner, meta.Repo, meta.CommentID, content)
	default:
		return nil
	}
	return err
}
