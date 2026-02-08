package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/icholy/xagent/internal/model"
	"gotest.tools/v3/assert"
)

func TestExtractGitHubWebhookEvent(t *testing.T) {
	t.Run("IssueComment", func(t *testing.T) {
		event := &github.IssueCommentEvent{
			Comment: &github.IssueComment{
				Body: strPtr("xagent: do something"),
				User: &github.User{
					ID:    int64Ptr(123),
					Login: strPtr("testuser"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: strPtr("https://github.com/owner/repo/issues/1"),
			},
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got != nil)
		assert.Equal(t, got.description, "A comment was made on an issue")
		assert.Equal(t, got.data, "xagent: do something")
		assert.Equal(t, got.url, "https://github.com/owner/repo/issues/1")
		assert.Equal(t, got.githubUserID, int64(123))
		assert.Equal(t, got.githubUsername, "testuser")
	})

	t.Run("IssueComment_PullRequest", func(t *testing.T) {
		event := &github.IssueCommentEvent{
			Comment: &github.IssueComment{
				Body: strPtr("xagent: review this"),
				User: &github.User{
					ID:    int64Ptr(456),
					Login: strPtr("pruser"),
				},
			},
			Issue: &github.Issue{
				HTMLURL:         strPtr("https://github.com/owner/repo/pull/2"),
				PullRequestLinks: &github.PullRequestLinks{},
			},
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got != nil)
		assert.Equal(t, got.description, "A comment was made on a pull request")
	})

	t.Run("IssueComment_NoXAgentPrefix", func(t *testing.T) {
		event := &github.IssueCommentEvent{
			Comment: &github.IssueComment{
				Body: strPtr("just a regular comment"),
				User: &github.User{
					ID:    int64Ptr(123),
					Login: strPtr("testuser"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: strPtr("https://github.com/owner/repo/issues/1"),
			},
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got == nil)
	})

	t.Run("IssueComment_NilFields", func(t *testing.T) {
		event := &github.IssueCommentEvent{
			Comment: nil,
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got == nil)
	})

	t.Run("PullRequestReviewComment", func(t *testing.T) {
		event := &github.PullRequestReviewCommentEvent{
			Comment: &github.PullRequestComment{
				Body: strPtr("xagent: fix this"),
				User: &github.User{
					ID:    int64Ptr(789),
					Login: strPtr("reviewer"),
				},
			},
			PullRequest: &github.PullRequest{
				HTMLURL: strPtr("https://github.com/owner/repo/pull/3"),
			},
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got != nil)
		assert.Equal(t, got.description, "A review comment was made on a pull request")
		assert.Equal(t, got.data, "xagent: fix this")
		assert.Equal(t, got.url, "https://github.com/owner/repo/pull/3")
		assert.Equal(t, got.githubUserID, int64(789))
		assert.Equal(t, got.githubUsername, "reviewer")
	})

	t.Run("PullRequestReviewComment_NoXAgentPrefix", func(t *testing.T) {
		event := &github.PullRequestReviewCommentEvent{
			Comment: &github.PullRequestComment{
				Body: strPtr("looks good"),
				User: &github.User{
					ID:    int64Ptr(789),
					Login: strPtr("reviewer"),
				},
			},
			PullRequest: &github.PullRequest{
				HTMLURL: strPtr("https://github.com/owner/repo/pull/3"),
			},
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got == nil)
	})

	t.Run("PullRequestReviewComment_NilFields", func(t *testing.T) {
		event := &github.PullRequestReviewCommentEvent{
			Comment: nil,
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got == nil)
	})

	t.Run("PullRequestReview_Submitted", func(t *testing.T) {
		event := &github.PullRequestReviewEvent{
			Action: strPtr("submitted"),
			Review: &github.PullRequestReview{
				Body: strPtr("xagent: please address comments"),
				User: &github.User{
					ID:    int64Ptr(101),
					Login: strPtr("lead"),
				},
			},
			PullRequest: &github.PullRequest{
				HTMLURL: strPtr("https://github.com/owner/repo/pull/4"),
			},
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got != nil)
		assert.Equal(t, got.description, "A review was submitted on a pull request")
		assert.Equal(t, got.data, "xagent: please address comments")
		assert.Equal(t, got.url, "https://github.com/owner/repo/pull/4")
		assert.Equal(t, got.githubUserID, int64(101))
		assert.Equal(t, got.githubUsername, "lead")
	})

	t.Run("PullRequestReview_NotSubmitted", func(t *testing.T) {
		event := &github.PullRequestReviewEvent{
			Action: strPtr("edited"),
			Review: &github.PullRequestReview{
				Body: strPtr("xagent: something"),
				User: &github.User{
					ID:    int64Ptr(101),
					Login: strPtr("lead"),
				},
			},
			PullRequest: &github.PullRequest{
				HTMLURL: strPtr("https://github.com/owner/repo/pull/4"),
			},
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got == nil)
	})

	t.Run("PullRequestReview_NoXAgentPrefix", func(t *testing.T) {
		event := &github.PullRequestReviewEvent{
			Action: strPtr("submitted"),
			Review: &github.PullRequestReview{
				Body: strPtr("approved"),
				User: &github.User{
					ID:    int64Ptr(101),
					Login: strPtr("lead"),
				},
			},
			PullRequest: &github.PullRequest{
				HTMLURL: strPtr("https://github.com/owner/repo/pull/4"),
			},
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got == nil)
	})

	t.Run("PullRequestReview_NilFields", func(t *testing.T) {
		event := &github.PullRequestReviewEvent{
			Action: nil,
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got == nil)
	})

	t.Run("UnknownEventType", func(t *testing.T) {
		event := &github.PushEvent{}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got == nil)
	})

	t.Run("WhitespacePrefix", func(t *testing.T) {
		event := &github.IssueCommentEvent{
			Comment: &github.IssueComment{
				Body: strPtr("  xagent: trimmed"),
				User: &github.User{
					ID:    int64Ptr(123),
					Login: strPtr("testuser"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: strPtr("https://github.com/owner/repo/issues/1"),
			},
		}
		got := extractGitHubWebhookEvent(event)
		assert.Assert(t, got != nil)
		assert.Equal(t, got.data, "xagent: trimmed")
	})
}

// signPayload signs a JSON payload with the given secret using HMAC-SHA256.
func signPayload(t *testing.T, payload []byte, secret string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// makeWebhookRequest creates an HTTP request that mimics a GitHub webhook delivery.
func makeWebhookRequest(t *testing.T, eventType string, payload any, secret string) *http.Request {
	t.Helper()
	body, err := json.Marshal(payload)
	assert.NilError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("X-GitHub-Delivery", "test-delivery-id")
	if secret != "" {
		req.Header.Set("X-Hub-Signature-256", signPayload(t, body, secret))
	}
	return req
}

func TestHandleGitHubWebhook(t *testing.T) {
	s := setupTestServer(t)
	webhookSecret := "test-webhook-secret"
	s.github = &GitHubConfig{
		WebhookSecret: webhookSecret,
	}

	ctx := randomUserID(t)
	userID := s.userID(ctx)

	// Create a GitHub account linked to this user
	account := &model.GitHubAccount{
		Owner:          userID,
		GitHubUserID:   943597,
		GitHubUsername: "icholy",
	}
	err := s.store.CreateGitHubAccount(ctx, nil, account)
	assert.NilError(t, err)

	// Create a task and link for event routing
	task := &model.Task{
		Name:      "test-task",
		Workspace: "test",
		Status:    model.TaskStatusCompleted,
		Command:   model.TaskCommandNone,
		Version:   1,
		Owner:     userID,
	}
	err = s.store.CreateTask(ctx, nil, task)
	assert.NilError(t, err)

	link := &model.Link{
		TaskID:    task.ID,
		Relevance: "test",
		URL:       "https://github.com/owner/repo/pull/10",
		Notify:    true,
	}
	err = s.store.CreateLink(ctx, nil, link)
	assert.NilError(t, err)

	t.Run("IssueComment_RoutesToTask", func(t *testing.T) {
		payload := github.IssueCommentEvent{
			Action: strPtr("created"),
			Comment: &github.IssueComment{
				Body: strPtr("xagent: please fix the tests"),
				User: &github.User{
					ID:    int64Ptr(943597),
					Login: strPtr("icholy"),
				},
			},
			Issue: &github.Issue{
				HTMLURL:         strPtr("https://github.com/owner/repo/pull/10"),
				PullRequestLinks: &github.PullRequestLinks{},
			},
		}
		req := makeWebhookRequest(t, "issue_comment", payload, webhookSecret)
		rec := httptest.NewRecorder()
		s.handleGitHubWebhook(rec, req)

		assert.Equal(t, rec.Code, http.StatusOK)
		assert.Equal(t, rec.Body.String(), "processed")

		// Verify the task was started (event routed to it)
		updatedTask, err := s.store.GetTask(ctx, nil, task.ID, userID)
		assert.NilError(t, err)
		assert.Equal(t, updatedTask.Status, model.TaskStatusPending)
	})

	t.Run("InvalidSignature", func(t *testing.T) {
		payload := github.IssueCommentEvent{
			Comment: &github.IssueComment{
				Body: strPtr("xagent: test"),
				User: &github.User{
					ID:    int64Ptr(943597),
					Login: strPtr("icholy"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: strPtr("https://github.com/owner/repo/issues/1"),
			},
		}
		req := makeWebhookRequest(t, "issue_comment", payload, "wrong-secret")
		rec := httptest.NewRecorder()
		s.handleGitHubWebhook(rec, req)

		assert.Equal(t, rec.Code, http.StatusBadRequest)
	})

	t.Run("IgnoredEventType", func(t *testing.T) {
		payload := github.PushEvent{}
		req := makeWebhookRequest(t, "push", payload, webhookSecret)
		rec := httptest.NewRecorder()
		s.handleGitHubWebhook(rec, req)

		assert.Equal(t, rec.Code, http.StatusOK)
		assert.Equal(t, rec.Body.String(), "ignored")
	})

	t.Run("NoLinkedAccount", func(t *testing.T) {
		payload := github.IssueCommentEvent{
			Action: strPtr("created"),
			Comment: &github.IssueComment{
				Body: strPtr("xagent: test"),
				User: &github.User{
					ID:    int64Ptr(999999),
					Login: strPtr("unknown"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: strPtr("https://github.com/owner/repo/issues/1"),
			},
		}
		req := makeWebhookRequest(t, "issue_comment", payload, webhookSecret)
		rec := httptest.NewRecorder()
		s.handleGitHubWebhook(rec, req)

		assert.Equal(t, rec.Code, http.StatusOK)
		assert.Equal(t, rec.Body.String(), "no linked account")
	})

	t.Run("NoXAgentPrefix_Ignored", func(t *testing.T) {
		payload := github.IssueCommentEvent{
			Action: strPtr("created"),
			Comment: &github.IssueComment{
				Body: strPtr("just a regular comment"),
				User: &github.User{
					ID:    int64Ptr(943597),
					Login: strPtr("icholy"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: strPtr("https://github.com/owner/repo/issues/1"),
			},
		}
		req := makeWebhookRequest(t, "issue_comment", payload, webhookSecret)
		rec := httptest.NewRecorder()
		s.handleGitHubWebhook(rec, req)

		assert.Equal(t, rec.Code, http.StatusOK)
		assert.Equal(t, rec.Body.String(), "ignored")
	})

	t.Run("MissingSignature", func(t *testing.T) {
		payload := github.IssueCommentEvent{
			Comment: &github.IssueComment{
				Body: strPtr("xagent: test"),
				User: &github.User{
					ID:    int64Ptr(943597),
					Login: strPtr("icholy"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: strPtr("https://github.com/owner/repo/issues/1"),
			},
		}
		body, err := json.Marshal(payload)
		assert.NilError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "issue_comment")
		req.Header.Set("X-GitHub-Delivery", "test-delivery-id")
		// No signature header
		rec := httptest.NewRecorder()
		s.handleGitHubWebhook(rec, req)

		assert.Equal(t, rec.Code, http.StatusBadRequest)
	})

	t.Run("UsernameUpdate", func(t *testing.T) {
		payload := github.IssueCommentEvent{
			Action: strPtr("created"),
			Comment: &github.IssueComment{
				Body: strPtr("xagent: test username update"),
				User: &github.User{
					ID:    int64Ptr(943597),
					Login: strPtr("icholy-renamed"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: strPtr("https://github.com/owner/repo/issues/99"),
			},
		}
		req := makeWebhookRequest(t, "issue_comment", payload, webhookSecret)
		rec := httptest.NewRecorder()
		s.handleGitHubWebhook(rec, req)

		assert.Equal(t, rec.Code, http.StatusOK)
		assert.Equal(t, rec.Body.String(), "processed")

		// Verify the username was updated
		updatedAccount, err := s.store.GetGitHubAccountByGitHubUserID(ctx, nil, 943597)
		assert.NilError(t, err)
		assert.Equal(t, updatedAccount.GitHubUsername, "icholy-renamed")
	})
}

func strPtr(s string) *string  { return &s }
func int64Ptr(i int64) *int64  { return &i }
