package githubx

import (
	"context"

	"github.com/shurcooL/githubv4"
)

// AddReaction adds a reaction to any reactable GraphQL node — an issue, pull
// request, comment, or review summary — addressed by its global node ID, via
// the addReaction mutation.
//
// The GraphQL API accepts every reactable subject uniformly. The REST
// Reactions API does not: it has no endpoint for some reactable types (a review
// summary, for one), so reacting over GraphQL is the only way to cover them all
// with a single code path.
func AddReaction(ctx context.Context, client *githubv4.Client, subjectID string, content githubv4.ReactionContent) error {
	var mutation struct {
		AddReaction struct {
			// A mutation must select at least one field; clientMutationId is the
			// cheapest, and its value is discarded.
			ClientMutationID githubv4.String
		} `graphql:"addReaction(input: $input)"`
	}
	input := githubv4.AddReactionInput{
		SubjectID: githubv4.ID(subjectID),
		Content:   content,
	}
	return client.Mutate(ctx, &mutation, input, nil)
}
