package githubserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/go-github/v88/github"
	"github.com/icholy/xagent/internal/eventrouter"
)

// react adds a reaction to the resource that triggered the outcome. It is a
// plain synchronous function: it does the work and returns an error. It owns no
// concurrency or lifetime policy — the WebhookHandler glue runs it in a
// goroutine and logs the error. Comment events react to the triggering comment;
// assignment events react to the issue/PR itself. Returns nil (not an error)
// when there's nothing to do: a non-GitHub Meta, an event with no reactable
// target, an org with no installation, or a non-reactable event type.
func (s *Server) react(ctx context.Context, outcome eventrouter.RouteOutcome) error {
	meta, ok := outcome.Input.Meta.(GitHubMeta)
	if !ok {
		return nil
	}
	// Decide whether this event has a reactable target before doing any work
	// (the org lookup is a DB round-trip). Comment events need a CommentID;
	// assignment events need an issue/PR Number; review submissions need a
	// ReviewNodeID. Everything else has no reactable target.
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
	// Build a client authenticated as the App's bot identity, backed by the
	// cached auto-refreshing installation transport so the reaction is
	// attributed to the App and repeated calls skip re-minting the token.
	client := s.tokens.Client(org.GitHubInstallationID)

	// Pick the emoji from the routing outcome: a created task gets 🚀, a woken
	// task 👀, and an event that matched a rule but created or woke nothing 😕.
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
	case EventTypeIssueAssigned, EventTypePullRequestAssigned:
		// PRs are issues for the Reactions API, so both assignment kinds react
		// to the issue/PR body via the issue endpoint.
		_, _, err = client.Reactions.CreateIssueReaction(ctx, meta.Owner, meta.Repo, meta.Number, content)
	case EventTypePullRequestReview:
		// Review summaries have no REST reaction endpoint, so react over GraphQL
		// by the review's global node ID.
		err = createReviewReaction(ctx, client, meta.ReviewNodeID, content)
	}
	return err
}

// githubGraphQLURL is the GraphQL endpoint. It is a var so tests can point it
// at an httptest server.
var githubGraphQLURL = "https://api.github.com/graphql"

// graphQLReactionContent maps the REST reaction names used above to the
// uppercase ReactionContent enum values the GraphQL API expects.
var graphQLReactionContent = map[string]string{
	"rocket":   "ROCKET",
	"eyes":     "EYES",
	"confused": "CONFUSED",
}

// createReviewReaction adds a reaction to a pull request review. The REST
// Reactions API exposes no endpoint for review summaries, so this issues the
// GraphQL addReaction mutation, which accepts any Reactable node — including
// PullRequestReview — addressed by its global node ID. It reuses the
// installation-authenticated HTTP client from the go-github client so the
// reaction is attributed to the App.
func createReviewReaction(ctx context.Context, client *github.Client, nodeID, content string) error {
	gqlContent, ok := graphQLReactionContent[content]
	if !ok {
		return fmt.Errorf("unsupported reaction content %q", content)
	}
	payload, err := json.Marshal(map[string]any{
		"query": "mutation($subjectId:ID!,$content:ReactionContent!)" +
			"{addReaction(input:{subjectId:$subjectId,content:$content}){clientMutationId}}",
		"variables": map[string]string{
			"subjectId": nodeID,
			"content":   gqlContent,
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubGraphQLURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github graphql addReaction: status %d: %s", resp.StatusCode, body)
	}
	// GraphQL returns HTTP 200 even when the mutation fails, so inspect the
	// errors array to surface failures the status code hides.
	var result struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("github graphql addReaction: decode response: %w", err)
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("github graphql addReaction: %s", result.Errors[0].Message)
	}
	return nil
}
