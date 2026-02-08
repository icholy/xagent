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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v68/github"
	"github.com/icholy/xagent/internal/model"
	"gotest.tools/v3/assert"
)

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
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
				},
			},
			expected: &githubWebhookEvent{
				description:    "A comment was made on an issue",
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
					HTMLURL:          github.Ptr("https://github.com/owner/repo/pull/2"),
					PullRequestLinks: &github.PullRequestLinks{},
				},
			},
			expected: &githubWebhookEvent{
				description:    "A comment was made on a pull request",
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
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/3"),
				},
			},
			expected: &githubWebhookEvent{
				description:    "A review comment was made on a pull request",
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
					HTMLURL: github.Ptr("https://github.com/owner/repo/pull/4"),
				},
			},
			expected: &githubWebhookEvent{
				description:    "A review was submitted on a pull request",
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
					HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
				},
			},
			expected: &githubWebhookEvent{
				description:    "A comment was made on an issue",
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
			Action: github.Ptr("created"),
			Comment: &github.IssueComment{
				Body: github.Ptr("xagent: please fix the tests"),
				User: &github.User{
					ID:    github.Ptr[int64](943597),
					Login: github.Ptr("icholy"),
				},
			},
			Issue: &github.Issue{
				HTMLURL:         github.Ptr("https://github.com/owner/repo/pull/10"),
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
				Body: github.Ptr("xagent: test"),
				User: &github.User{
					ID:    github.Ptr[int64](943597),
					Login: github.Ptr("icholy"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
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
		req := makeWebhookRequest(t, "issue_comment", payload, webhookSecret)
		rec := httptest.NewRecorder()
		s.handleGitHubWebhook(rec, req)

		assert.Equal(t, rec.Code, http.StatusOK)
		assert.Equal(t, rec.Body.String(), "no linked account")
	})

	t.Run("NoXAgentPrefix_Ignored", func(t *testing.T) {
		payload := github.IssueCommentEvent{
			Action: github.Ptr("created"),
			Comment: &github.IssueComment{
				Body: github.Ptr("just a regular comment"),
				User: &github.User{
					ID:    github.Ptr[int64](943597),
					Login: github.Ptr("icholy"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
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
				Body: github.Ptr("xagent: test"),
				User: &github.User{
					ID:    github.Ptr[int64](943597),
					Login: github.Ptr("icholy"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: github.Ptr("https://github.com/owner/repo/issues/1"),
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
			Action: github.Ptr("created"),
			Comment: &github.IssueComment{
				Body: github.Ptr("xagent: test username update"),
				User: &github.User{
					ID:    github.Ptr[int64](943597),
					Login: github.Ptr("icholy-renamed"),
				},
			},
			Issue: &github.Issue{
				HTMLURL: github.Ptr("https://github.com/owner/repo/issues/99"),
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
