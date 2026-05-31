package githubserver

import (
	"context"

	"github.com/google/go-github/v68/github"
	"github.com/icholy/xagent/internal/eventrouter"
)

// reactionEmoji picks the acknowledgement emoji from the routing outcome: a
// created task gets 🚀 ("rocket"), a woken task 👀 ("eyes"), and a comment that
// matched a rule but created or woke nothing gets 😕 ("confused").
func reactionEmoji(outcome eventrouter.RouteOutcome) string {
	switch {
	case outcome.Created:
		return "rocket"
	case len(outcome.TaskIDs) > 0:
		return "eyes"
	default:
		return "confused"
	}
}

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
	// Mint an installation token so the reaction is attributed to the App's bot
	// identity, then build a client from it.
	token, err := s.CreateInstallationToken(ctx, org.GitHubInstallationID)
	if err != nil {
		return err
	}
	client := github.NewClient(nil).WithAuthToken(token.Token)

	content := reactionEmoji(outcome)
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
