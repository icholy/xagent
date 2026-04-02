package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v68/github"
	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
	"gotest.tools/v3/assert"
)

// setupTestStore creates a test store with a clean database.
// Requires TEST_DATABASE_URL environment variable to be set.
func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := store.Open(dsn, false)
	assert.NilError(t, err)
	t.Cleanup(func() { db.Close() })
	return store.New(db)
}

// createTestUser creates a user with an org and returns the context, user ID, and org ID.
func createTestUser(t *testing.T, s *store.Store) (userID string, orgID int64) {
	t.Helper()
	userID = uuid.NewString()
	err := s.CreateUser(t.Context(), nil, &model.User{
		ID:    userID,
		Email: userID + "@test.com",
		Name:  "Test User",
	})
	assert.NilError(t, err)
	org := &model.Org{
		Name:  "test-org-" + userID,
		Owner: userID,
	}
	err = s.CreateOrg(t.Context(), nil, org)
	assert.NilError(t, err)
	err = s.AddOrgMember(t.Context(), nil, &model.OrgMember{
		OrgID:  org.ID,
		UserID: userID,
		Role:   "owner",
	})
	assert.NilError(t, err)
	err = s.UpdateDefaultOrgID(t.Context(), nil, userID, org.ID)
	assert.NilError(t, err)
	return userID, org.ID
}

func TestExtractGitHubWebhookEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    any
		expected *githubWebhookEvent
	}{
		{
			name: "IssueComment",
			event: &github.IssueCommentEvent{
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
			expected: &githubWebhookEvent{
				description:    "testuser commented on issue #1",
				data:           "xagent: do something",
				url:            "https://github.com/owner/repo/issues/1",
				githubUserID:   123,
				githubUsername: "testuser",
			},
		},
		{
			name: "IssueComment_PullRequest",
			event: &github.IssueCommentEvent{
				Comment: &github.IssueComment{
					Body: github.Ptr("xagent: review this"),
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
			},
			expected: &githubWebhookEvent{
				description:    "pruser commented on PR #2",
				data:           "xagent: review this",
				url:            "https://github.com/owner/repo/pull/2",
				githubUserID:   456,
				githubUsername: "pruser",
			},
		},
		{
			name: "IssueComment_NoXAgentPrefix",
			event: &github.IssueCommentEvent{
				Comment: &github.IssueComment{
					Body: github.Ptr("just a regular comment"),
					User: &github.User{
						ID:    github.Ptr[int64](123),
						Login: github.Ptr("testuser"),
					},
				},
				Issue: &github.Issue{
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
				},
			},
			expected: nil,
		},
		{
			name:     "IssueComment_NilFields",
			event:    &github.IssueCommentEvent{Comment: nil},
			expected: nil,
		},
		{
			name: "PullRequestReviewComment",
			event: &github.PullRequestReviewCommentEvent{
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
			expected: &githubWebhookEvent{
				description:    "reviewer reviewed PR #3",
				data:           "xagent: fix this",
				url:            "https://github.com/owner/repo/pull/3",
				githubUserID:   789,
				githubUsername: "reviewer",
			},
		},
		{
			name: "PullRequestReviewComment_NoXAgentPrefix",
			event: &github.PullRequestReviewCommentEvent{
				Comment: &github.PullRequestComment{
					Body: github.Ptr("looks good"),
					User: &github.User{
						ID:    github.Ptr[int64](789),
						Login: github.Ptr("reviewer"),
					},
				},
				PullRequest: &github.PullRequest{
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/3"),
				},
			},
			expected: nil,
		},
		{
			name:     "PullRequestReviewComment_NilFields",
			event:    &github.PullRequestReviewCommentEvent{Comment: nil},
			expected: nil,
		},
		{
			name: "PullRequestReview_Submitted",
			event: &github.PullRequestReviewEvent{
				Action: github.Ptr("submitted"),
				Review: &github.PullRequestReview{
					Body: github.Ptr("xagent: please address comments"),
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
			expected: &githubWebhookEvent{
				description:    "lead reviewed PR #4",
				data:           "xagent: please address comments",
				url:            "https://github.com/owner/repo/pull/4",
				githubUserID:   101,
				githubUsername: "lead",
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
					Body: github.Ptr("approved"),
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
				Comment: &github.IssueComment{
					Body: github.Ptr("  xagent: trimmed"),
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
			expected: &githubWebhookEvent{
				description:    "testuser commented on issue #1",
				data:           "xagent: trimmed",
				url:            "https://github.com/owner/repo/issues/1",
				githubUserID:   123,
				githubUsername: "testuser",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGitHubWebhookEvent(tt.event)
			assert.DeepEqual(t, got, tt.expected, cmp.AllowUnexported(githubWebhookEvent{}))
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

func setupTestHandler(t *testing.T) (*GitHubHandler, *store.Store, *RouterMock) {
	t.Helper()
	s := setupTestStore(t)
	r := &RouterMock{
		RouteFunc: func(ctx context.Context, event eventrouter.Event) (int, error) {
			return 1, nil
		},
	}
	h := &GitHubHandler{
		Router: r,
		Store:  s,
	}
	return h, s, r
}

func TestHandleGitHubWebhookRoutesToTask(t *testing.T) {
	h, s, r := setupTestHandler(t)

	userID, orgID := createTestUser(t, s)
	ctx := apiauth.WithUser(t.Context(), &apiauth.UserInfo{ID: userID, OrgID: orgID})

	ghUserID := rand.Int64N(1<<53) + 1
	err := s.LinkGitHubAccount(ctx, nil, userID, ghUserID, "testuser")
	assert.NilError(t, err)

	payload := github.IssueCommentEvent{
		Action: github.Ptr("created"),
		Comment: &github.IssueComment{
			Body: github.Ptr("xagent: please fix the tests"),
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

	calls := r.RouteCalls()
	assert.Equal(t, len(calls), 1)
	assert.DeepEqual(t, calls[0].Event, eventrouter.Event{
		Type:        eventrouter.EventTypeGitHub,
		Description: "testuser commented on PR #10",
		Data:        "xagent: please fix the tests",
		URL:         "https://github.com/owner/repo/pull/10",
		UserID:      userID,
	})
}

func TestHandleGitHubWebhookRoutesWithCorrectEvent(t *testing.T) {
	h, s, r := setupTestHandler(t)

	userID, orgID := createTestUser(t, s)
	ctx := apiauth.WithUser(t.Context(), &apiauth.UserInfo{ID: userID, OrgID: orgID})

	ghUserID := rand.Int64N(1<<53) + 1
	err := s.LinkGitHubAccount(ctx, nil, userID, ghUserID, "testuser")
	assert.NilError(t, err)

	prURL := "https://github.com/owner/repo/pull/42"
	payload := github.IssueCommentEvent{
		Action: github.Ptr("created"),
		Comment: &github.IssueComment{
			Body: github.Ptr("xagent: deploy please"),
			User: &github.User{
				ID:    github.Ptr(ghUserID),
				Login: github.Ptr("testuser"),
			},
		},
		Issue: &github.Issue{
			Number:           github.Ptr(42),
			HTMLURL:          github.Ptr(prURL),
			PullRequestLinks: &github.PullRequestLinks{},
		},
	}
	req := makeWebhookRequest(t, "issue_comment", payload)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "processed")

	calls := r.RouteCalls()
	assert.Equal(t, len(calls), 1)
	assert.DeepEqual(t, calls[0].Event, eventrouter.Event{
		Type:        eventrouter.EventTypeGitHub,
		Description: "testuser commented on PR #42",
		Data:        "xagent: deploy please",
		URL:         prURL,
		UserID:      userID,
	})
}

func TestHandleGitHubWebhookIgnoredEventType(t *testing.T) {
	h, _, _ := setupTestHandler(t)

	payload := github.PushEvent{}
	req := makeWebhookRequest(t, "push", payload)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "ignored")
}

func TestHandleGitHubWebhookNoLinkedAccount(t *testing.T) {
	h, _, _ := setupTestHandler(t)

	payload := github.IssueCommentEvent{
		Action: github.Ptr("created"),
		Comment: &github.IssueComment{
			Body: github.Ptr("xagent: test"),
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
