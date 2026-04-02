package webhook

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/atlassian"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
	"gotest.tools/v3/assert"
)

func TestExtractAtlassianWebhookEvent(t *testing.T) {
	tests := []struct {
		name     string
		payload  atlassian.WebhookPayload
		expected *atlassianWebhookEvent
	}{
		{
			name: "CommentCreated",
			payload: atlassian.WebhookPayload{
				WebhookEvent: "comment_created",
				Comment: &atlassian.Comment{
					Body:   "xagent: do something",
					Author: atlassian.User{AccountID: "abc123", DisplayName: "Test User"},
				},
				Issue: &atlassian.Issue{
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
			name: "NoXAgentPrefix",
			payload: atlassian.WebhookPayload{
				WebhookEvent: "comment_created",
				Comment: &atlassian.Comment{
					Body:   "just a regular comment",
					Author: atlassian.User{AccountID: "abc123", DisplayName: "Test User"},
				},
				Issue: &atlassian.Issue{
					Key:  "PROJ-123",
					Self: "https://mycompany.atlassian.net/rest/api/2/issue/12345",
				},
			},
			expected: nil,
		},
		{
			name: "NilComment",
			payload: atlassian.WebhookPayload{
				WebhookEvent: "comment_created",
				Comment:      nil,
				Issue: &atlassian.Issue{
					Key:  "PROJ-123",
					Self: "https://mycompany.atlassian.net/rest/api/2/issue/12345",
				},
			},
			expected: nil,
		},
		{
			name: "NilIssue",
			payload: atlassian.WebhookPayload{
				WebhookEvent: "comment_created",
				Comment: &atlassian.Comment{
					Body:   "xagent: test",
					Author: atlassian.User{AccountID: "abc123", DisplayName: "Test User"},
				},
				Issue: nil,
			},
			expected: nil,
		},
		{
			name: "UnknownEventType",
			payload: atlassian.WebhookPayload{
				WebhookEvent: "issue_updated",
			},
			expected: nil,
		},
		{
			name: "CommentUpdatedIgnored",
			payload: atlassian.WebhookPayload{
				WebhookEvent: "comment_updated",
				Comment: &atlassian.Comment{
					Body:   "xagent: test",
					Author: atlassian.User{AccountID: "abc123", DisplayName: "Test User"},
				},
				Issue: &atlassian.Issue{
					Key:  "PROJ-123",
					Self: "https://mycompany.atlassian.net/rest/api/2/issue/12345",
				},
			},
			expected: nil,
		},
		{
			name: "WhitespacePrefix",
			payload: atlassian.WebhookPayload{
				WebhookEvent: "comment_created",
				Comment: &atlassian.Comment{
					Body:   "  xagent: trimmed",
					Author: atlassian.User{AccountID: "abc123", DisplayName: "Test User"},
				},
				Issue: &atlassian.Issue{
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

func makeAtlassianWebhookRequest(t *testing.T, orgID int64, payload any, secret string) *http.Request {
	t.Helper()
	body, err := json.Marshal(payload)
	assert.NilError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/webhook/atlassian?org="+strconv.FormatInt(orgID, 10), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature", atlassian.SignWebhook(body, secret))
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

	jiraIssueURL := "https://mycompany.atlassian.net/browse/PROJ-10"
	link := &model.Link{
		TaskID:    task.ID,
		Relevance: "test",
		URL:       jiraIssueURL,
		Notify:    true,
	}
	err = s.CreateLink(ctx, nil, link)
	assert.NilError(t, err)

	payload := atlassian.WebhookPayload{
		WebhookEvent: "comment_created",
		Comment: &atlassian.Comment{
			Body:   "xagent: please fix the tests",
			Author: atlassian.User{AccountID: atlassianAccountID, DisplayName: "Test User"},
		},
		Issue: &atlassian.Issue{
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

	payload := atlassian.WebhookPayload{
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

	payload := atlassian.WebhookPayload{
		WebhookEvent: "comment_created",
		Comment: &atlassian.Comment{
			Body:   "xagent: test",
			Author: atlassian.User{AccountID: "unknown-account", DisplayName: "Unknown"},
		},
		Issue: &atlassian.Issue{
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

	payload := atlassian.WebhookPayload{WebhookEvent: "comment_created"}
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
