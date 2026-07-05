package githubserver

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v88/github"
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/x/testx"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestToGithubInputEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    any
		expected *eventrouter.InputEvent
	}{
		{
			name: "IssueComment",
			event: &github.IssueCommentEvent{
				Action: github.Ptr("created"),
				Comment: &github.IssueComment{
					ID:      github.Ptr[int64](555),
					NodeID:  github.Ptr("IC_node555"),
					Body:    github.Ptr("xagent: do something"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1#issuecomment-555"),
					User: &github.User{
						ID:    github.Ptr[int64](123),
						Login: github.Ptr("testuser"),
					},
				},
				Issue: &github.Issue{
					Number:  github.Ptr(1),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
				},
				Repo: &github.Repository{
					Name:  github.Ptr("repo"),
					Owner: &github.User{Login: github.Ptr("owner")},
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "issue_comment",
				Description: "testuser commented on issue #1",
				Data:        "xagent: do something",
				URL:         "https://github.com/owner/repo/issues/1#issuecomment-555",
				Attrs:       eventrouter.Attrs{"mention": nil},
				Meta: GitHubMeta{
					AuthorID:    123,
					AuthorLogin: "testuser",
					NodeID:      "IC_node555",
				},
			},
		},
		{
			name: "IssueComment_PullRequest",
			event: &github.IssueCommentEvent{
				Action: github.Ptr("created"),
				Comment: &github.IssueComment{
					ID:      github.Ptr[int64](556),
					NodeID:  github.Ptr("IC_node556"),
					Body:    github.Ptr("xagent: review this"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/2#issuecomment-556"),
					User: &github.User{
						ID:    github.Ptr[int64](456),
						Login: github.Ptr("pruser"),
					},
				},
				Issue: &github.Issue{
					Number:           github.Ptr(2),
					HTMLURL:          github.Ptr("https://github.com/owner/repo/pull/2"),
					PullRequestLinks: &github.PullRequestLinks{},
				},
				Repo: &github.Repository{
					Name:  github.Ptr("repo"),
					Owner: &github.User{Login: github.Ptr("owner")},
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "issue_comment",
				Description: "pruser commented on PR #2",
				Data:        "xagent: review this",
				URL:         "https://github.com/owner/repo/pull/2#issuecomment-556",
				Attrs:       eventrouter.Attrs{"mention": nil},
				Meta: GitHubMeta{
					AuthorID:    456,
					AuthorLogin: "pruser",
					NodeID:      "IC_node556",
				},
			},
		},
		{
			name: "IssueComment_NoXAgentPrefix",
			event: &github.IssueCommentEvent{
				Action: github.Ptr("created"),
				Comment: &github.IssueComment{
					Body:    github.Ptr("just a regular comment"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1#issuecomment-600"),
					User: &github.User{
						ID:    github.Ptr[int64](123),
						Login: github.Ptr("testuser"),
					},
				},
				Issue: &github.Issue{
					Number:  github.Ptr(1),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "issue_comment",
				Description: "testuser commented on issue #1",
				Data:        "just a regular comment",
				URL:         "https://github.com/owner/repo/issues/1#issuecomment-600",
				Attrs:       eventrouter.Attrs{"mention": nil},
				Meta:        GitHubMeta{AuthorID: 123, AuthorLogin: "testuser"},
			},
		},
		{
			name: "IssueComment_SingleMention",
			event: &github.IssueCommentEvent{
				Action: github.Ptr("created"),
				Comment: &github.IssueComment{
					Body:    github.Ptr("hey @icholy-bot please take a look"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1#issuecomment-610"),
					User: &github.User{
						ID:    github.Ptr[int64](123),
						Login: github.Ptr("testuser"),
					},
				},
				Issue: &github.Issue{
					Number:  github.Ptr(1),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "issue_comment",
				Description: "testuser commented on issue #1",
				Data:        "hey @icholy-bot please take a look",
				URL:         "https://github.com/owner/repo/issues/1#issuecomment-610",
				Attrs:       eventrouter.Attrs{"mention": {"icholy-bot"}},
				Meta:        GitHubMeta{AuthorID: 123, AuthorLogin: "testuser"},
			},
		},
		{
			name: "IssueComment_MultipleMentions",
			event: &github.IssueCommentEvent{
				Action: github.Ptr("created"),
				Comment: &github.IssueComment{
					// Adjacent mentions and a punctuation-terminated one exercise the
					// non-consuming boundary check. "@alice/team" is a team ref, not a
					// login mention, so it is excluded.
					Body:    github.Ptr("(@alice) @bob, cc @carol! see @alice/team"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1#issuecomment-611"),
					User: &github.User{
						ID:    github.Ptr[int64](123),
						Login: github.Ptr("testuser"),
					},
				},
				Issue: &github.Issue{
					Number:  github.Ptr(1),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "issue_comment",
				Description: "testuser commented on issue #1",
				Data:        "(@alice) @bob, cc @carol! see @alice/team",
				URL:         "https://github.com/owner/repo/issues/1#issuecomment-611",
				Attrs:       eventrouter.Attrs{"mention": {"alice", "bob", "carol"}},
				Meta:        GitHubMeta{AuthorID: 123, AuthorLogin: "testuser"},
			},
		},
		{
			name:     "IssueComment_NilFields",
			event:    &github.IssueCommentEvent{Comment: nil},
			expected: nil,
		},
		{
			name: "IssueComment_Edited",
			event: &github.IssueCommentEvent{
				Action: github.Ptr("edited"),
				Comment: &github.IssueComment{
					Body:    github.Ptr("xagent: do something"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1#issuecomment-601"),
					User: &github.User{
						ID:    github.Ptr[int64](123),
						Login: github.Ptr("testuser"),
					},
				},
				Issue: &github.Issue{
					Number:  github.Ptr(1),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "issue_comment",
				Description: "testuser commented on issue #1",
				Data:        "xagent: do something",
				URL:         "https://github.com/owner/repo/issues/1#issuecomment-601",
				Attrs:       eventrouter.Attrs{"mention": nil},
				Meta:        GitHubMeta{AuthorID: 123, AuthorLogin: "testuser"},
			},
		},
		{
			name: "IssueComment_Deleted",
			event: &github.IssueCommentEvent{
				Action: github.Ptr("deleted"),
				Comment: &github.IssueComment{
					Body: github.Ptr("xagent: do something"),
					User: &github.User{
						ID:    github.Ptr[int64](123),
						Login: github.Ptr("testuser"),
					},
				},
				Issue: &github.Issue{
					Number:  github.Ptr(1),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
				},
			},
			expected: nil,
		},
		{
			name: "PullRequestReviewComment",
			event: &github.PullRequestReviewCommentEvent{
				Action: github.Ptr("created"),
				Comment: &github.PullRequestComment{
					ID:      github.Ptr[int64](777),
					NodeID:  github.Ptr("PRRC_node777"),
					Body:    github.Ptr("xagent: fix this"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/3#discussion_r777"),
					User: &github.User{
						ID:    github.Ptr[int64](789),
						Login: github.Ptr("reviewer"),
					},
				},
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(3),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/3"),
				},
				Repo: &github.Repository{
					Name:  github.Ptr("repo"),
					Owner: &github.User{Login: github.Ptr("owner")},
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "pull_request_review_comment",
				Description: "reviewer reviewed PR #3",
				Data:        "xagent: fix this",
				URL:         "https://github.com/owner/repo/pull/3#discussion_r777",
				Attrs:       eventrouter.Attrs{"mention": nil},
				Meta: GitHubMeta{
					AuthorID:    789,
					AuthorLogin: "reviewer",
					NodeID:      "PRRC_node777",
				},
			},
		},
		{
			name: "PullRequestReviewComment_NoXAgentPrefix",
			event: &github.PullRequestReviewCommentEvent{
				Action: github.Ptr("created"),
				Comment: &github.PullRequestComment{
					Body:    github.Ptr("looks good"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/3#discussion_r800"),
					User: &github.User{
						ID:    github.Ptr[int64](789),
						Login: github.Ptr("reviewer"),
					},
				},
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(3),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/3"),
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "pull_request_review_comment",
				Description: "reviewer reviewed PR #3",
				Data:        "looks good",
				URL:         "https://github.com/owner/repo/pull/3#discussion_r800",
				Attrs:       eventrouter.Attrs{"mention": nil},
				Meta:        GitHubMeta{AuthorID: 789, AuthorLogin: "reviewer"},
			},
		},
		{
			name:     "PullRequestReviewComment_NilFields",
			event:    &github.PullRequestReviewCommentEvent{Comment: nil},
			expected: nil,
		},
		{
			name: "PullRequestReviewComment_Edited",
			event: &github.PullRequestReviewCommentEvent{
				Action: github.Ptr("edited"),
				Comment: &github.PullRequestComment{
					Body:    github.Ptr("xagent: fix this"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/3#discussion_r801"),
					User: &github.User{
						ID:    github.Ptr[int64](789),
						Login: github.Ptr("reviewer"),
					},
				},
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(3),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/3"),
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "pull_request_review_comment",
				Description: "reviewer reviewed PR #3",
				Data:        "xagent: fix this",
				URL:         "https://github.com/owner/repo/pull/3#discussion_r801",
				Attrs:       eventrouter.Attrs{"mention": nil},
				Meta:        GitHubMeta{AuthorID: 789, AuthorLogin: "reviewer"},
			},
		},
		{
			name: "PullRequestReviewComment_Deleted",
			event: &github.PullRequestReviewCommentEvent{
				Action: github.Ptr("deleted"),
				Comment: &github.PullRequestComment{
					Body: github.Ptr("xagent: fix this"),
					User: &github.User{
						ID:    github.Ptr[int64](789),
						Login: github.Ptr("reviewer"),
					},
				},
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(3),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/3"),
				},
			},
			expected: nil,
		},
		{
			name: "PullRequestReview_Submitted",
			event: &github.PullRequestReviewEvent{
				Action: github.Ptr("submitted"),
				Review: &github.PullRequestReview{
					NodeID:  github.Ptr("PRR_node123"),
					Body:    github.Ptr("xagent: please address comments"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/4#pullrequestreview-123"),
					User: &github.User{
						ID:    github.Ptr[int64](101),
						Login: github.Ptr("lead"),
					},
				},
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(4),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/4"),
				},
				Repo: &github.Repository{
					Name:  github.Ptr("repo"),
					Owner: &github.User{Login: github.Ptr("owner")},
				},
			},
			// The reactable target is the review summary, addressed by its
			// GraphQL node ID.
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "pull_request_review",
				Description: "lead reviewed PR #4",
				Data:        "xagent: please address comments",
				URL:         "https://github.com/owner/repo/pull/4#pullrequestreview-123",
				Attrs:       eventrouter.Attrs{"mention": nil},
				Meta:        GitHubMeta{AuthorID: 101, AuthorLogin: "lead", NodeID: "PRR_node123"},
			},
		},
		{
			name: "PullRequestReview_NotSubmitted",
			event: &github.PullRequestReviewEvent{
				Action: github.Ptr("edited"),
				Review: &github.PullRequestReview{
					Body: github.Ptr("xagent: something"),
					User: &github.User{
						ID:    github.Ptr[int64](101),
						Login: github.Ptr("lead"),
					},
				},
				PullRequest: &github.PullRequest{
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/4"),
				},
			},
			expected: nil,
		},
		{
			name: "PullRequestReview_NoXAgentPrefix",
			event: &github.PullRequestReviewEvent{
				Action: github.Ptr("submitted"),
				Review: &github.PullRequestReview{
					Body:    github.Ptr("approved"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/4#pullrequestreview-200"),
					User: &github.User{
						ID:    github.Ptr[int64](101),
						Login: github.Ptr("lead"),
					},
				},
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(4),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/4"),
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "pull_request_review",
				Description: "lead reviewed PR #4",
				Data:        "approved",
				URL:         "https://github.com/owner/repo/pull/4#pullrequestreview-200",
				Attrs:       eventrouter.Attrs{"mention": nil},
				Meta:        GitHubMeta{AuthorID: 101, AuthorLogin: "lead"},
			},
		},
		{
			name:     "PullRequestReview_NilFields",
			event:    &github.PullRequestReviewEvent{Action: nil},
			expected: nil,
		},
		{
			name:     "UnknownEventType",
			event:    &github.PushEvent{},
			expected: nil,
		},
		{
			name: "WhitespacePrefix",
			event: &github.IssueCommentEvent{
				Action: github.Ptr("created"),
				Comment: &github.IssueComment{
					Body:    github.Ptr("  xagent: trimmed"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1#issuecomment-602"),
					User: &github.User{
						ID:    github.Ptr[int64](123),
						Login: github.Ptr("testuser"),
					},
				},
				Issue: &github.Issue{
					Number:  github.Ptr(1),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "issue_comment",
				Description: "testuser commented on issue #1",
				Data:        "xagent: trimmed",
				URL:         "https://github.com/owner/repo/issues/1#issuecomment-602",
				Attrs:       eventrouter.Attrs{"mention": nil},
				Meta:        GitHubMeta{AuthorID: 123, AuthorLogin: "testuser"},
			},
		},
		{
			name: "IssuesEvent_Assigned",
			event: &github.IssuesEvent{
				Action: github.Ptr("assigned"),
				Issue: &github.Issue{
					Number:  github.Ptr(7),
					NodeID:  github.Ptr("I_node7"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/7"),
				},
				Assignee: &github.User{
					Login: github.Ptr("icholy-bot"),
				},
				Sender: &github.User{
					ID:    github.Ptr[int64](999),
					Login: github.Ptr("octocat"),
				},
				Repo: &github.Repository{
					Name:  github.Ptr("repo"),
					Owner: &github.User{Login: github.Ptr("owner")},
				},
			},
			// The reactable target is the issue itself, addressed by its GraphQL
			// node ID.
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "issue_assigned",
				Description: "octocat assigned issue #7 to @icholy-bot",
				URL:         "https://github.com/owner/repo/issues/7",
				Assignee:    "icholy-bot",
				Attrs:       eventrouter.Attrs{"assignee": {"icholy-bot"}},
				Meta: GitHubMeta{
					AuthorID:    999,
					AuthorLogin: "octocat",
					NodeID:      "I_node7",
				},
			},
		},
		{
			name: "IssuesEvent_WrongAction",
			event: &github.IssuesEvent{
				Action: github.Ptr("opened"),
				Issue: &github.Issue{
					Number:  github.Ptr(7),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/7"),
				},
				Assignee: &github.User{Login: github.Ptr("icholy-bot")},
				Sender: &github.User{
					ID:    github.Ptr[int64](999),
					Login: github.Ptr("octocat"),
				},
			},
			expected: nil,
		},
		{
			name: "IssuesEvent_NoSender",
			event: &github.IssuesEvent{
				Action: github.Ptr("assigned"),
				Issue: &github.Issue{
					Number:  github.Ptr(7),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/7"),
				},
				Assignee: &github.User{Login: github.Ptr("icholy-bot")},
			},
			expected: nil,
		},
		{
			name: "PullRequestEvent_Assigned",
			event: &github.PullRequestEvent{
				Action: github.Ptr("assigned"),
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(12),
					NodeID:  github.Ptr("PR_node12"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/12"),
				},
				Assignee: &github.User{
					Login: github.Ptr("icholy-bot"),
				},
				Sender: &github.User{
					ID:    github.Ptr[int64](42),
					Login: github.Ptr("alice"),
				},
				Repo: &github.Repository{
					Name:  github.Ptr("repo"),
					Owner: &github.User{Login: github.Ptr("owner")},
				},
			},
			// The reactable target is the PR itself, addressed by its GraphQL
			// node ID.
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "pull_request_assigned",
				Description: "alice assigned PR #12 to @icholy-bot",
				URL:         "https://github.com/owner/repo/pull/12",
				Assignee:    "icholy-bot",
				Attrs:       eventrouter.Attrs{"assignee": {"icholy-bot"}},
				Meta: GitHubMeta{
					AuthorID:    42,
					AuthorLogin: "alice",
					NodeID:      "PR_node12",
				},
			},
		},
		{
			name: "PullRequestEvent_WrongAction",
			event: &github.PullRequestEvent{
				Action: github.Ptr("synchronize"),
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(12),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/12"),
				},
				Assignee: &github.User{Login: github.Ptr("icholy-bot")},
				Sender: &github.User{
					ID:    github.Ptr[int64](42),
					Login: github.Ptr("alice"),
				},
			},
			expected: nil,
		},
		{
			name: "PullRequestEvent_NoSender",
			event: &github.PullRequestEvent{
				Action: github.Ptr("assigned"),
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(12),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/12"),
				},
				Assignee: &github.User{Login: github.Ptr("icholy-bot")},
			},
			expected: nil,
		},
		{
			name: "IssuesEvent_Labeled",
			event: &github.IssuesEvent{
				Action: github.Ptr("labeled"),
				Issue: &github.Issue{
					Number:  github.Ptr(7),
					NodeID:  github.Ptr("I_node7"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/7"),
				},
				Label: &github.Label{Name: github.Ptr("xagent")},
				Sender: &github.User{
					ID:    github.Ptr[int64](999),
					Login: github.Ptr("octocat"),
				},
				Repo: &github.Repository{
					Name:  github.Ptr("repo"),
					Owner: &github.User{Login: github.Ptr("owner")},
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "label_added",
				Description: `octocat labeled issue #7 "xagent"`,
				Values:      []string{"xagent"},
				Attrs:       eventrouter.Attrs{"label": {"xagent"}},
				URL:         "https://github.com/owner/repo/issues/7",
				Meta: GitHubMeta{
					AuthorID:    999,
					AuthorLogin: "octocat",
					NodeID:      "I_node7",
				},
			},
		},
		{
			name: "IssuesEvent_Labeled_NoLabel",
			event: &github.IssuesEvent{
				Action: github.Ptr("labeled"),
				Issue: &github.Issue{
					Number:  github.Ptr(7),
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/7"),
				},
				Sender: &github.User{
					ID:    github.Ptr[int64](999),
					Login: github.Ptr("octocat"),
				},
			},
			expected: nil,
		},
		{
			name: "PullRequestEvent_Labeled",
			event: &github.PullRequestEvent{
				Action: github.Ptr("labeled"),
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(12),
					NodeID:  github.Ptr("PR_node12"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/12"),
				},
				Label: &github.Label{Name: github.Ptr("needs-review")},
				Sender: &github.User{
					ID:    github.Ptr[int64](42),
					Login: github.Ptr("alice"),
				},
				Repo: &github.Repository{
					Name:  github.Ptr("repo"),
					Owner: &github.User{Login: github.Ptr("owner")},
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "label_added",
				Description: `alice labeled PR #12 "needs-review"`,
				Values:      []string{"needs-review"},
				Attrs:       eventrouter.Attrs{"label": {"needs-review"}},
				URL:         "https://github.com/owner/repo/pull/12",
				Meta: GitHubMeta{
					AuthorID:    42,
					AuthorLogin: "alice",
					NodeID:      "PR_node12",
				},
			},
		},
		{
			name: "PullRequestEvent_Labeled_NoSender",
			event: &github.PullRequestEvent{
				Action: github.Ptr("labeled"),
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(12),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/12"),
				},
				Label: &github.Label{Name: github.Ptr("needs-review")},
			},
			expected: nil,
		},
		{
			name: "PullRequestEvent_Closed_Merged",
			event: &github.PullRequestEvent{
				Action: github.Ptr("closed"),
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(12),
					NodeID:  github.Ptr("PR_node12"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/12"),
					Merged:  github.Ptr(true),
				},
				Sender: &github.User{
					ID:    github.Ptr[int64](42),
					Login: github.Ptr("alice"),
				},
				Repo: &github.Repository{
					Name:  github.Ptr("repo"),
					Owner: &github.User{Login: github.Ptr("owner")},
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "pull_request_closed",
				Description: "alice merged PR #12",
				Data:        "merged",
				Attrs:       eventrouter.Attrs{"state": {"merged"}},
				URL:         "https://github.com/owner/repo/pull/12",
				Meta: GitHubMeta{
					AuthorID:    42,
					AuthorLogin: "alice",
					NodeID:      "PR_node12",
				},
			},
		},
		{
			name: "PullRequestEvent_Closed_NotMerged",
			event: &github.PullRequestEvent{
				Action: github.Ptr("closed"),
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(12),
					NodeID:  github.Ptr("PR_node12"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/12"),
					Merged:  github.Ptr(false),
				},
				Sender: &github.User{
					ID:    github.Ptr[int64](42),
					Login: github.Ptr("alice"),
				},
				Repo: &github.Repository{
					Name:  github.Ptr("repo"),
					Owner: &github.User{Login: github.Ptr("owner")},
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "pull_request_closed",
				Description: "alice closed PR #12",
				Data:        "closed",
				Attrs:       eventrouter.Attrs{"state": {"closed"}},
				URL:         "https://github.com/owner/repo/pull/12",
				Meta: GitHubMeta{
					AuthorID:    42,
					AuthorLogin: "alice",
					NodeID:      "PR_node12",
				},
			},
		},
		{
			name: "PullRequestEvent_Closed_NoSender",
			event: &github.PullRequestEvent{
				Action: github.Ptr("closed"),
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(12),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/12"),
					Merged:  github.Ptr(true),
				},
			},
			expected: nil,
		},
		{
			name: "PullRequestEvent_Opened",
			event: &github.PullRequestEvent{
				Action: github.Ptr("opened"),
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(12),
					NodeID:  github.Ptr("PR_node12"),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/12"),
				},
				Sender: &github.User{
					ID:    github.Ptr[int64](42),
					Login: github.Ptr("alice"),
				},
				Repo: &github.Repository{
					Name:  github.Ptr("repo"),
					Owner: &github.User{Login: github.Ptr("owner")},
				},
			},
			expected: &eventrouter.InputEvent{
				Source:      "github",
				Type:        "pull_request_opened",
				Description: "alice opened PR #12",
				URL:         "https://github.com/owner/repo/pull/12",
				Meta: GitHubMeta{
					AuthorID:    42,
					AuthorLogin: "alice",
					NodeID:      "PR_node12",
				},
			},
		},
		{
			name: "PullRequestEvent_Opened_NoSender",
			event: &github.PullRequestEvent{
				Action: github.Ptr("opened"),
				PullRequest: &github.PullRequest{
					Number:  github.Ptr(12),
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/12"),
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toInputEvent(tt.event)
			assert.DeepEqual(t, got, tt.expected)
		})
	}
}

// makeWebhookRequest creates an HTTP request that mimics a GitHub webhook delivery.
func makeWebhookRequest(t *testing.T, eventType string, payload any) *http.Request {
	t.Helper()
	body, err := json.Marshal(payload)
	assert.NilError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("X-GitHub-Delivery", "test-delivery-id")
	return req
}

func TestHandleGitHubWebhookRoutesToTask(t *testing.T) {
	var ghUserID int64 = 12345
	router := &RouterMock{
		RouteFunc: func(ctx context.Context, input eventrouter.InputEvent) (int, error) {
			return 1, nil
		},
	}
	h := &WebhookHandler{
		Router: router,
		Store: &StoreMock{
			GetUserByGitHubUserIDFunc: func(ctx context.Context, tx *sql.Tx, id int64) (*model.User, error) {
				if id == ghUserID {
					return &model.User{ID: "user-1", GitHubUserID: ghUserID, GitHubUsername: "testuser"}, nil
				}
				return nil, sql.ErrNoRows
			},
			UpdateGitHubUsernameFunc: func(ctx context.Context, tx *sql.Tx, id int64, username string) error {
				return nil
			},
		},
	}

	payload := github.IssueCommentEvent{
		Action: github.Ptr("created"),
		Comment: &github.IssueComment{
			Body:    github.Ptr("xagent: please fix the tests"),
			HTMLURL: github.Ptr("https://github.com/owner/repo/pull/10#issuecomment-1000"),
			User: &github.User{
				ID:    github.Ptr(ghUserID),
				Login: github.Ptr("testuser"),
			},
		},
		Issue: &github.Issue{
			Number:           github.Ptr(10),
			HTMLURL:          github.Ptr("https://github.com/owner/repo/pull/10"),
			PullRequestLinks: &github.PullRequestLinks{},
		},
	}
	req := makeWebhookRequest(t, "issue_comment", payload)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "processed")

	calls := router.RouteCalls()
	assert.Assert(t, cmp.Len(calls, 1))
	assert.DeepEqual(t, calls[0].Input, eventrouter.InputEvent{
		Source:      "github",
		Type:        "issue_comment",
		Description: "testuser commented on PR #10",
		Data:        "xagent: please fix the tests",
		URL:         "https://github.com/owner/repo/pull/10#issuecomment-1000",
		Attrs:       eventrouter.Attrs{"mention": nil},
		UserID:      "user-1",
		Meta:        GitHubMeta{AuthorID: ghUserID, AuthorLogin: "testuser"},
	})
}

func TestHandleGitHubWebhookIgnoredEventType(t *testing.T) {
	h := &WebhookHandler{}

	payload := github.PushEvent{}
	req := makeWebhookRequest(t, "push", payload)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "ignored")
}

// installation "created" no longer records any state: linking is verified on
// demand against live GitHub membership, so the handler ignores it.
func TestHandleGitHubWebhookInstallationCreated(t *testing.T) {
	store := &StoreMock{}
	h := &WebhookHandler{Store: store}

	payload := github.InstallationEvent{
		Action: github.Ptr("created"),
		Installation: &github.Installation{
			ID: github.Ptr[int64](42),
			Account: &github.User{
				Login: github.Ptr("acme"),
				Type:  github.Ptr("Organization"),
			},
		},
		Sender: &github.User{
			ID: github.Ptr[int64](777),
		},
	}
	req := makeWebhookRequest(t, "installation", payload)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "ignored")
	assert.Assert(t, cmp.Len(store.ClearGitHubInstallationCalls(), 0))
}

func TestHandleGitHubWebhookInstallationDeleted(t *testing.T) {
	store := &StoreMock{
		ClearGitHubInstallationFunc: func(ctx context.Context, tx *sql.Tx, installationID int64) error {
			return nil
		},
	}
	h := &WebhookHandler{Store: store}

	payload := github.InstallationEvent{
		Action: github.Ptr("deleted"),
		Installation: &github.Installation{
			ID: github.Ptr[int64](42),
		},
	}
	req := makeWebhookRequest(t, "installation", payload)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "cleared")
	clears := store.ClearGitHubInstallationCalls()
	assert.DeepEqual(t, testx.ExtractField(clears, "InstallationID"), []int64{42})
}

func TestHandleGitHubWebhookInstallationOtherAction(t *testing.T) {
	store := &StoreMock{}
	h := &WebhookHandler{Store: store}

	payload := github.InstallationEvent{
		Action: github.Ptr("suspend"),
		Installation: &github.Installation{
			ID: github.Ptr[int64](42),
		},
	}
	req := makeWebhookRequest(t, "installation", payload)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "ignored")
	assert.Assert(t, cmp.Len(store.ClearGitHubInstallationCalls(), 0))
}

func TestHandleGitHubWebhookNoLinkedAccount(t *testing.T) {
	h := &WebhookHandler{
		Store: &StoreMock{
			GetUserByGitHubUserIDFunc: func(ctx context.Context, tx *sql.Tx, id int64) (*model.User, error) {
				return nil, sql.ErrNoRows
			},
		},
	}

	payload := github.IssueCommentEvent{
		Action: github.Ptr("created"),
		Comment: &github.IssueComment{
			Body:    github.Ptr("xagent: test"),
			HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1#issuecomment-2000"),
			User: &github.User{
				ID:    github.Ptr[int64](999999),
				Login: github.Ptr("unknown"),
			},
		},
		Issue: &github.Issue{
			HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
		},
	}
	req := makeWebhookRequest(t, "issue_comment", payload)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "no linked account")
}
