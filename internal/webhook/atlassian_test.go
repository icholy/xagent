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

func makeJiraComment(body, accountID, displayName string) *struct {
	Body   string `json:"body"`
	Author struct {
		AccountID   string `json:"accountId"`
		DisplayName string `json:"displayName"`
	} `json:"author"`
} {
	c := &struct {
		Body   string `json:"body"`
		Author struct {
			AccountID   string `json:"accountId"`
			DisplayName string `json:"displayName"`
		} `json:"author"`
	}{
		Body: body,
	}
	c.Author.AccountID = accountID
	c.Author.DisplayName = displayName
	return c
}

func makeJiraIssue(key, selfLink string) *struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
	} `json:"fields"`
	Self string `json:"self"`
} {
	return &struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
		} `json:"fields"`
		Self string `json:"self"`
	}{
		Key:  key,
		Self: selfLink,
	}
}

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
				Comment:      makeJiraComment("xagent: do something", "abc123", "Test User"),
				Issue:        makeJiraIssue("PROJ-123", "https://mycompany.atlassian.net/rest/api/2/issue/12345"),
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
			payload: jiraWebhookPayload{
				WebhookEvent: "comment_created",
				Comment:      makeJiraComment("just a regular comment", "abc123", "Test User"),
				Issue:        makeJiraIssue("PROJ-123", "https://mycompany.atlassian.net/rest/api/2/issue/12345"),
			},
			expected: nil,
		},
		{
			name: "NilComment",
			payload: jiraWebhookPayload{
				WebhookEvent: "comment_created",
				Comment:      nil,
				Issue:        makeJiraIssue("PROJ-123", "https://mycompany.atlassian.net/rest/api/2/issue/12345"),
			},
			expected: nil,
		},
		{
			name: "NilIssue",
			payload: jiraWebhookPayload{
				WebhookEvent: "comment_created",
				Comment:      makeJiraComment("xagent: test", "abc123", "Test User"),
				Issue:        nil,
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
			name: "CommentUpdatedIgnored",
			payload: jiraWebhookPayload{
				WebhookEvent: "comment_updated",
				Comment:      makeJiraComment("xagent: test", "abc123", "Test User"),
				Issue:        makeJiraIssue("PROJ-123", "https://mycompany.atlassian.net/rest/api/2/issue/12345"),
			},
			expected: nil,
		},
		{
			name: "WhitespacePrefix",
			payload: jiraWebhookPayload{
				WebhookEvent: "comment_created",
				Comment:      makeJiraComment("  xagent: trimmed", "abc123", "Test User"),
				Issue:        makeJiraIssue("PROJ-1", "https://mycompany.atlassian.net/rest/api/2/issue/1"),
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

	tests := []struct {
		name      string
		signature string
		errMsg    string
	}{
		{
			name:      "ValidSignature",
			signature: validSig,
			errMsg:    "",
		},
		{
			name:      "InvalidSignature",
			signature: "sha256=deadbeef",
			errMsg:    "signature mismatch",
		},
		{
			name:      "MissingSignature",
			signature: "",
			errMsg:    "missing X-Hub-Signature header",
		},
		{
			name:      "UnsupportedFormat",
			signature: "sha1=abc",
			errMsg:    "unsupported signature format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyAtlassianSignature(body, tt.signature, secret)
			if tt.errMsg == "" {
				assert.NilError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.errMsg)
			}
		})
	}
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
		Comment:      makeJiraComment("xagent: please fix the tests", atlassianAccountID, "Test User"),
		Issue:        makeJiraIssue("PROJ-10", "https://mycompany.atlassian.net/rest/api/2/issue/10"),
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
		Comment:      makeJiraComment("xagent: test", "unknown-account", "Unknown"),
		Issue:        makeJiraIssue("PROJ-1", "https://mycompany.atlassian.net/rest/api/2/issue/1"),
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
