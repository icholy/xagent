package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
	"gotest.tools/v3/assert"
)

func TestExtractAtlassianWebhookEvent(t *testing.T) {
	tests := []struct {
		name     string
		payload  jiraWebhookPayload
		expected *atlassianWebhookEvent
	}{
		{
			name: "CommentCreated",
			payload: jiraWebhookPayload{
				WebhookEvent: "comment_created",
				Comment: &struct {
					Body   string `json:"body"`
					Author struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					} `json:"author"`
					UpdateAuthor struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					} `json:"updateAuthor"`
				}{
					Body: "xagent: do something",
					Author: struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					}{AccountID: "abc123", DisplayName: "Test User"},
				},
				Issue: &struct {
					Key    string `json:"key"`
					Fields struct {
						Summary string `json:"summary"`
					} `json:"fields"`
					Self string `json:"self"`
				}{
					Key:  "PROJ-123",
					Self: "https://mycompany.atlassian.net/rest/api/2/issue/12345",
				},
			},
			expected: &atlassianWebhookEvent{
				description:        "Test User commented on PROJ-123",
				data:               "xagent: do something",
				url:                "https://mycompany.atlassian.net/browse/PROJ-123",
				atlassianAccountID: "abc123",
			},
		},
		{
			name: "CommentUpdated_UsesUpdateAuthor",
			payload: jiraWebhookPayload{
				WebhookEvent: "comment_updated",
				Comment: &struct {
					Body   string `json:"body"`
					Author struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					} `json:"author"`
					UpdateAuthor struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					} `json:"updateAuthor"`
				}{
					Body: "xagent: updated instructions",
					Author: struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					}{AccountID: "original-author", DisplayName: "Original Author"},
					UpdateAuthor: struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					}{AccountID: "updater123", DisplayName: "Updater"},
				},
				Issue: &struct {
					Key    string `json:"key"`
					Fields struct {
						Summary string `json:"summary"`
					} `json:"fields"`
					Self string `json:"self"`
				}{
					Key:  "PROJ-456",
					Self: "https://mycompany.atlassian.net/rest/api/2/issue/67890",
				},
			},
			expected: &atlassianWebhookEvent{
				description:        "Updater updated comment on PROJ-456",
				data:               "xagent: updated instructions",
				url:                "https://mycompany.atlassian.net/browse/PROJ-456",
				atlassianAccountID: "updater123",
			},
		},
		{
			name: "NoXAgentPrefix",
			payload: jiraWebhookPayload{
				WebhookEvent: "comment_created",
				Comment: &struct {
					Body   string `json:"body"`
					Author struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					} `json:"author"`
					UpdateAuthor struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					} `json:"updateAuthor"`
				}{
					Body: "just a regular comment",
					Author: struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					}{AccountID: "abc123", DisplayName: "Test User"},
				},
				Issue: &struct {
					Key    string `json:"key"`
					Fields struct {
						Summary string `json:"summary"`
					} `json:"fields"`
					Self string `json:"self"`
				}{
					Key:  "PROJ-123",
					Self: "https://mycompany.atlassian.net/rest/api/2/issue/12345",
				},
			},
			expected: nil,
		},
		{
			name: "NilComment",
			payload: jiraWebhookPayload{
				WebhookEvent: "comment_created",
				Comment:      nil,
				Issue: &struct {
					Key    string `json:"key"`
					Fields struct {
						Summary string `json:"summary"`
					} `json:"fields"`
					Self string `json:"self"`
				}{
					Key:  "PROJ-123",
					Self: "https://mycompany.atlassian.net/rest/api/2/issue/12345",
				},
			},
			expected: nil,
		},
		{
			name: "NilIssue",
			payload: jiraWebhookPayload{
				WebhookEvent: "comment_created",
				Comment: &struct {
					Body   string `json:"body"`
					Author struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					} `json:"author"`
					UpdateAuthor struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					} `json:"updateAuthor"`
				}{
					Body: "xagent: test",
					Author: struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					}{AccountID: "abc123", DisplayName: "Test User"},
				},
				Issue: nil,
			},
			expected: nil,
		},
		{
			name: "UnknownEventType",
			payload: jiraWebhookPayload{
				WebhookEvent: "issue_updated",
			},
			expected: nil,
		},
		{
			name: "WhitespacePrefix",
			payload: jiraWebhookPayload{
				WebhookEvent: "comment_created",
				Comment: &struct {
					Body   string `json:"body"`
					Author struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					} `json:"author"`
					UpdateAuthor struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					} `json:"updateAuthor"`
				}{
					Body: "  xagent: trimmed",
					Author: struct {
						AccountID   string `json:"accountId"`
						DisplayName string `json:"displayName"`
					}{AccountID: "abc123", DisplayName: "Test User"},
				},
				Issue: &struct {
					Key    string `json:"key"`
					Fields struct {
						Summary string `json:"summary"`
					} `json:"fields"`
					Self string `json:"self"`
				}{
					Key:  "PROJ-1",
					Self: "https://mycompany.atlassian.net/rest/api/2/issue/1",
				},
			},
			expected: &atlassianWebhookEvent{
				description:        "Test User commented on PROJ-1",
				data:               "xagent: trimmed",
				url:                "https://mycompany.atlassian.net/browse/PROJ-1",
				atlassianAccountID: "abc123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(tt.payload)
			assert.NilError(t, err)
			got, err := extractAtlassianWebhookEvent(body)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tt.expected, cmp.AllowUnexported(atlassianWebhookEvent{}))
		})
	}
}

func TestVerifyAtlassianSignature(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"test": "payload"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	t.Run("ValidSignature", func(t *testing.T) {
		err := verifyAtlassianSignature(body, validSig, secret)
		assert.NilError(t, err)
	})

	t.Run("InvalidSignature", func(t *testing.T) {
		err := verifyAtlassianSignature(body, "sha256=deadbeef", secret)
		assert.ErrorContains(t, err, "signature mismatch")
	})

	t.Run("MissingSignature", func(t *testing.T) {
		err := verifyAtlassianSignature(body, "", secret)
		assert.ErrorContains(t, err, "missing X-Hub-Signature header")
	})

	t.Run("UnsupportedFormat", func(t *testing.T) {
		err := verifyAtlassianSignature(body, "sha1=abc", secret)
		assert.ErrorContains(t, err, "unsupported signature format")
	})
}

func TestIssueURL(t *testing.T) {
	tests := []struct {
		selfLink string
		issueKey string
		expected string
	}{
		{
			selfLink: "https://mycompany.atlassian.net/rest/api/2/issue/12345",
			issueKey: "PROJ-123",
			expected: "https://mycompany.atlassian.net/browse/PROJ-123",
		},
		{
			selfLink: "invalid-url",
			issueKey: "PROJ-123",
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.issueKey, func(t *testing.T) {
			got := issueURL(tt.selfLink, tt.issueKey)
			assert.Equal(t, got, tt.expected)
		})
	}
}

func signPayload(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func makeAtlassianWebhookRequest(t *testing.T, orgID int64, payload any, secret string) *http.Request {
	t.Helper()
	body, err := json.Marshal(payload)
	assert.NilError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/webhook/atlassian?org="+strconv.FormatInt(orgID, 10), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature", signPayload(body, secret))
	return req
}

func setupAtlassianTestHandler(t *testing.T) (*AtlassianHandler, *store.Store) {
	t.Helper()
	s := setupTestStore(t)
	h := &AtlassianHandler{
		Log:   slog.Default(),
		Store: s,
	}
	return h, s
}

func TestHandleAtlassianWebhookRoutesToTask(t *testing.T) {
	h, s := setupAtlassianTestHandler(t)

	userID, orgID := createTestUser(t, s)
	ctx := apiauth.WithUser(t.Context(), &apiauth.UserInfo{ID: userID, OrgID: orgID})

	atlassianAccountID := "atlassian-" + strconv.FormatInt(rand.Int64N(1<<53)+1, 10)
	err := s.LinkAtlassianAccount(ctx, nil, userID, atlassianAccountID, "Test User")
	assert.NilError(t, err)

	secret := "test-webhook-secret"
	err = s.SetOrgAtlassianWebhookSecret(ctx, nil, orgID, secret)
	assert.NilError(t, err)

	task := &model.Task{
		Name:      "test-task",
		Workspace: "test",
		Status:    model.TaskStatusCompleted,
		Command:   model.TaskCommandNone,
		Version:   1,
		OrgID:     orgID,
	}
	err = s.CreateTask(ctx, nil, task)
	assert.NilError(t, err)

	issueURL := "https://mycompany.atlassian.net/browse/PROJ-10"
	link := &model.Link{
		TaskID:    task.ID,
		Relevance: "test",
		URL:       issueURL,
		Notify:    true,
	}
	err = s.CreateLink(ctx, nil, link)
	assert.NilError(t, err)

	payload := jiraWebhookPayload{
		WebhookEvent: "comment_created",
		Comment: &struct {
			Body   string `json:"body"`
			Author struct {
				AccountID   string `json:"accountId"`
				DisplayName string `json:"displayName"`
			} `json:"author"`
			UpdateAuthor struct {
				AccountID   string `json:"accountId"`
				DisplayName string `json:"displayName"`
			} `json:"updateAuthor"`
		}{
			Body: "xagent: please fix the tests",
			Author: struct {
				AccountID   string `json:"accountId"`
				DisplayName string `json:"displayName"`
			}{AccountID: atlassianAccountID, DisplayName: "Test User"},
		},
		Issue: &struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
			} `json:"fields"`
			Self string `json:"self"`
		}{
			Key:  "PROJ-10",
			Self: "https://mycompany.atlassian.net/rest/api/2/issue/10",
		},
	}

	req := makeAtlassianWebhookRequest(t, orgID, payload, secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "processed")

	updatedTask, err := s.GetTask(ctx, nil, task.ID, orgID)
	assert.NilError(t, err)
	assert.Equal(t, updatedTask.Status, model.TaskStatusPending)
}

func TestHandleAtlassianWebhookIgnoredEventType(t *testing.T) {
	h, s := setupAtlassianTestHandler(t)

	_, orgID := createTestUser(t, s)
	secret := "test-webhook-secret"
	err := s.SetOrgAtlassianWebhookSecret(t.Context(), nil, orgID, secret)
	assert.NilError(t, err)

	payload := jiraWebhookPayload{
		WebhookEvent: "issue_updated",
	}
	req := makeAtlassianWebhookRequest(t, orgID, payload, secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "ignored")
}

func TestHandleAtlassianWebhookNoLinkedAccount(t *testing.T) {
	h, s := setupAtlassianTestHandler(t)

	_, orgID := createTestUser(t, s)
	secret := "test-webhook-secret"
	err := s.SetOrgAtlassianWebhookSecret(t.Context(), nil, orgID, secret)
	assert.NilError(t, err)

	payload := jiraWebhookPayload{
		WebhookEvent: "comment_created",
		Comment: &struct {
			Body   string `json:"body"`
			Author struct {
				AccountID   string `json:"accountId"`
				DisplayName string `json:"displayName"`
			} `json:"author"`
			UpdateAuthor struct {
				AccountID   string `json:"accountId"`
				DisplayName string `json:"displayName"`
			} `json:"updateAuthor"`
		}{
			Body: "xagent: test",
			Author: struct {
				AccountID   string `json:"accountId"`
				DisplayName string `json:"displayName"`
			}{AccountID: "unknown-account", DisplayName: "Unknown"},
		},
		Issue: &struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
			} `json:"fields"`
			Self string `json:"self"`
		}{
			Key:  "PROJ-1",
			Self: "https://mycompany.atlassian.net/rest/api/2/issue/1",
		},
	}
	req := makeAtlassianWebhookRequest(t, orgID, payload, secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "no linked account")
}

func TestHandleAtlassianWebhookInvalidSignature(t *testing.T) {
	h, s := setupAtlassianTestHandler(t)

	_, orgID := createTestUser(t, s)
	secret := "test-webhook-secret"
	err := s.SetOrgAtlassianWebhookSecret(t.Context(), nil, orgID, secret)
	assert.NilError(t, err)

	payload := jiraWebhookPayload{WebhookEvent: "comment_created"}
	body, err := json.Marshal(payload)
	assert.NilError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/webhook/atlassian?org="+strconv.FormatInt(orgID, 10), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature", "sha256=deadbeef")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusForbidden)
}

func TestHandleAtlassianWebhookMissingOrg(t *testing.T) {
	h, _ := setupAtlassianTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/webhook/atlassian", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusBadRequest)
}
