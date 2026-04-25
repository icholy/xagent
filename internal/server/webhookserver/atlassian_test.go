package webhookserver

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/icholy/xagent/internal/atlassian"
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
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
				eventType:          "comment_created",
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
			expected: &atlassianWebhookEvent{
				eventType:          "comment_created",
				description:        "Test User commented on PROJ-123",
				data:               "just a regular comment",
				url:                "https://mycompany.atlassian.net/browse/PROJ-123",
				atlassianAccountID: "abc123",
			},
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
				eventType:          "comment_created",
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

func TestHandleAtlassianWebhookRoutesToTask(t *testing.T) {
	secret := "test-webhook-secret"
	accountID := "atlassian-abc123"
	router := &RouterMock{
		RouteFunc: func(ctx context.Context, input eventrouter.InputEvent) (int, error) {
			return 1, nil
		},
	}
	h := &AtlassianHandler{
		Router: router,
		Store: &StoreMock{
			GetOrgAtlassianWebhookSecretFunc: func(ctx context.Context, tx *sql.Tx, orgID int64) (string, error) {
				return secret, nil
			},
			GetUserByAtlassianAccountIDFunc: func(ctx context.Context, tx *sql.Tx, id string) (*model.User, error) {
				if id == accountID {
					return &model.User{ID: "user-1"}, nil
				}
				return nil, sql.ErrNoRows
			},
		},
	}

	payload := atlassian.WebhookPayload{
		WebhookEvent: "comment_created",
		Comment: &atlassian.Comment{
			Body:   "xagent: please fix the tests",
			Author: atlassian.User{AccountID: accountID, DisplayName: "Test User"},
		},
		Issue: &atlassian.Issue{
			Key:  "PROJ-10",
			Self: "https://mycompany.atlassian.net/rest/api/2/issue/10",
		},
	}

	req := makeAtlassianWebhookRequest(t, 1, payload, secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "processed")

	calls := router.RouteCalls()
	assert.Equal(t, len(calls), 1)
	assert.DeepEqual(t, calls[0].Input, eventrouter.InputEvent{
		Source:      "atlassian",
		Type:        "comment_created",
		Description: "Test User commented on PROJ-10",
		Data:        "xagent: please fix the tests",
		URL:         "https://mycompany.atlassian.net/browse/PROJ-10",
		UserID:      "user-1",
	})
}

func TestHandleAtlassianWebhookIgnoredEventType(t *testing.T) {
	secret := "test-webhook-secret"
	h := &AtlassianHandler{
		Store: &StoreMock{
			GetOrgAtlassianWebhookSecretFunc: func(ctx context.Context, tx *sql.Tx, orgID int64) (string, error) {
				return secret, nil
			},
		},
	}

	payload := atlassian.WebhookPayload{
		WebhookEvent: "issue_updated",
	}
	req := makeAtlassianWebhookRequest(t, 1, payload, secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "ignored")
}

func TestHandleAtlassianWebhookNoLinkedAccount(t *testing.T) {
	secret := "test-webhook-secret"
	h := &AtlassianHandler{
		Store: &StoreMock{
			GetOrgAtlassianWebhookSecretFunc: func(ctx context.Context, tx *sql.Tx, orgID int64) (string, error) {
				return secret, nil
			},
			GetUserByAtlassianAccountIDFunc: func(ctx context.Context, tx *sql.Tx, id string) (*model.User, error) {
				return nil, sql.ErrNoRows
			},
		},
	}

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
	req := makeAtlassianWebhookRequest(t, 1, payload, secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, rec.Body.String(), "no linked account")
}

func TestHandleAtlassianWebhookInvalidSignature(t *testing.T) {
	secret := "test-webhook-secret"
	h := &AtlassianHandler{
		Store: &StoreMock{
			GetOrgAtlassianWebhookSecretFunc: func(ctx context.Context, tx *sql.Tx, orgID int64) (string, error) {
				return secret, nil
			},
		},
	}

	payload := atlassian.WebhookPayload{WebhookEvent: "comment_created"}
	body, err := json.Marshal(payload)
	assert.NilError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/webhook/atlassian?org=1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature", "sha256=deadbeef")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusForbidden)
}

func TestHandleAtlassianWebhookMissingOrg(t *testing.T) {
	h := &AtlassianHandler{}

	req := httptest.NewRequest(http.MethodPost, "/webhook/atlassian", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusBadRequest)
}
