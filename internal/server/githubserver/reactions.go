package githubserver

import (
	"context"

	"github.com/google/go-github/v88/github"
	"github.com/shurcooL/githubv4"

	"github.com/icholy/xagent/internal/eventrouter"
)

// react adds a reaction to the resource that triggered the outcome. It is a
// plain synchronous function: it does the work and returns an error. It owns no
// concurrency or lifetime policy — the WebhookHandler glue runs it in a
// goroutine and logs the error. Comment events react to the triggering comment;
// assignment events react to the issue/PR itself; review submissions react to
// the review summary. Returns nil (not an error) when there's nothing to do: a
// non-GitHub Meta, an event with no reactable target, an org with no
// installation, or a non-reactable event type.
func (s *Server) react(ctx context.Context, outcome eventrouter.RouteOutcome) error {
	meta, ok := outcome.Input.Meta.(GitHubMeta)
	if !ok {
		return nil
	}
	// Decide whether this event has a reactable target before doing any work
	// (the org lookup is a DB round-trip). Comment events need a CommentID;
	// assignment events need an issue/PR Number; review submissions need the
	// review's GraphQL node ID. Everything else has no reactable target.
	switch outcome.Input.Type {
	case EventTypeIssueComment, EventTypePullRequestReviewComment:
		if meta.CommentID == 0 {
			return nil
		}
	case EventTypeIssueAssigned, EventTypePullRequestAssigned:
		if meta.Number == 0 {
			return nil
		}
	case EventTypePullRequestReview:
		if meta.ReviewNodeID == "" {
			return nil
		}
	default:
		return nil
	}
	org, err := s.store.GetOrg(ctx, nil, outcome.OrgID)
	if err != nil {
		return err
	}
	if org.GitHubInstallationID == 0 {
		return nil
	}
	// An *http.Client authenticated as the App's bot identity, backed by the
	// cached auto-refreshing installation transport so the reaction is
	// attributed to the App and repeated calls skip re-minting the token. It
	// feeds both the go-github REST client and the githubv4 GraphQL client.
	httpClient := s.tokens.Client(org.GitHubInstallationID)

	// Pick the emoji from the routing outcome: a created task gets 🚀, a woken
	// task 👀, and an event that matched a rule but created or woke nothing 😕.
	content := reactionContent(outcome)

	// Review summaries can't be reacted to through the REST Reactions API, so
	// they go through the GraphQL addReaction mutation keyed by the node ID.
	if outcome.Input.Type == EventTypePullRequestReview {
		return addReaction(ctx, githubv4.NewClient(httpClient), meta.ReviewNodeID, content)
	}

	// WithHTTPClient only errors on a nil client, which never happens here.
	client, _ := github.NewClient(github.WithHTTPClient(httpClient))
	switch outcome.Input.Type {
	case EventTypeIssueComment:
		_, _, err = client.Reactions.CreateIssueCommentReaction(ctx, meta.Owner, meta.Repo, meta.CommentID, content)
	case EventTypePullRequestReviewComment:
		_, _, err = client.Reactions.CreatePullRequestCommentReaction(ctx, meta.Owner, meta.Repo, meta.CommentID, content)
	case EventTypeIssueAssigned, EventTypePullRequestAssigned:
		// PRs are issues for the Reactions API, so both assignment kinds react
		// to the issue/PR body via the issue endpoint.
		_, _, err = client.Reactions.CreateIssueReaction(ctx, meta.Owner, meta.Repo, meta.Number, content)
	}
	return err
}

// reactionContent maps a routing outcome to the go-github REST reaction string.
func reactionContent(outcome eventrouter.RouteOutcome) string {
	switch {
	case outcome.Created:
		return "rocket"
	case len(outcome.TaskIDs) > 0:
		return "eyes"
	default:
		return "confused"
	}
}

// addReaction adds a reaction to a GraphQL subject (e.g. a pull request review
// summary, which the REST Reactions API cannot target) via the addReaction
// mutation. content is a go-github REST reaction string, mapped to the
// equivalent GraphQL ReactionContent enum.
func addReaction(ctx context.Context, client *githubv4.Client, subjectID, content string) error {
	var mutation struct {
		AddReaction struct {
			// A mutation must select at least one field; clientMutationId is the
			// cheapest, and its value is discarded.
			ClientMutationID githubv4.String
		} `graphql:"addReaction(input: $input)"`
	}
	input := githubv4.AddReactionInput{
		SubjectID: githubv4.ID(subjectID),
		Content:   graphQLReactionContent(content),
	}
	return client.Mutate(ctx, &mutation, input, nil)
}

// graphQLReactionContent maps a go-github REST reaction string to the GraphQL
// ReactionContent enum. Only the emojis reactionContent emits are handled;
// anything else falls back to 😕, matching the REST "confused" default.
func graphQLReactionContent(content string) githubv4.ReactionContent {
	switch content {
	case "rocket":
		return githubv4.ReactionContentRocket
	case "eyes":
		return githubv4.ReactionContentEyes
	default:
		return githubv4.ReactionContentConfused
	}
}
