package githubserver

import (
	"testing"

	"github.com/shurcooL/githubv4"
	"gotest.tools/v3/assert"

	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
)

func TestReactionContent(t *testing.T) {
	tests := []struct {
		name    string
		outcome eventrouter.RouteOutcome
		content githubv4.ReactionContent
		ok      bool
	}{
		{
			name:    "created task gets rocket",
			outcome: eventrouter.RouteOutcome{Created: true, TaskIDs: []int64{1}, Rule: &model.RoutingRule{Wakeup: true}},
			content: githubv4.ReactionContentRocket,
			ok:      true,
		},
		{
			name:    "woken task gets eyes",
			outcome: eventrouter.RouteOutcome{TaskIDs: []int64{1}, Rule: &model.RoutingRule{Wakeup: true}},
			content: githubv4.ReactionContentEyes,
			ok:      true,
		},
		{
			name:    "attached task on non-waking rule gets eyes",
			outcome: eventrouter.RouteOutcome{TaskIDs: []int64{1}, Rule: &model.RoutingRule{Wakeup: false}},
			content: githubv4.ReactionContentEyes,
			ok:      true,
		},
		{
			name:    "matched-only waking rule gets confused",
			outcome: eventrouter.RouteOutcome{Rule: &model.RoutingRule{Wakeup: true}},
			content: githubv4.ReactionContentConfused,
			ok:      true,
		},
		{
			name:    "matched-only non-waking rule reacts with nothing",
			outcome: eventrouter.RouteOutcome{Rule: &model.RoutingRule{Wakeup: false}},
			ok:      false,
		},
		{
			name:    "matched-only nil rule reacts with nothing",
			outcome: eventrouter.RouteOutcome{},
			ok:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, ok := reactionContent(tt.outcome)
			assert.Equal(t, ok, tt.ok)
			if tt.ok {
				assert.Equal(t, content, tt.content)
			}
		})
	}
}
