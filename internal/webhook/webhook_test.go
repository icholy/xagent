package webhook_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/icholy/xagent/internal/webhook"
	"gotest.tools/v3/assert"
)

func loadGithubWebhook(t *testing.T, filename string) *http.Request {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", filename))
	assert.NilError(t, err)

	var testEvent struct {
		Headers map[string]string `json:"headers"`
		Payload json.RawMessage   `json:"payload"`
	}
	assert.NilError(t, json.Unmarshal(data, &testEvent))

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(testEvent.Payload))
	for k, v := range testEvent.Headers {
		req.Header.Set(k, v)
	}
	return req
}

func TestGitHubPullRequestReviewComment(t *testing.T) {
	req := loadGithubWebhook(t, "pr_review_event.json")

	publisher := &PublisherMock{
		PublishFunc: func(event *webhook.Event) error {
			return nil
		},
	}

	handler := webhook.NewHandler(&webhook.Config{
		Publisher: publisher,
		NoVerify:  true,
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, rec.Code, http.StatusOK)
	assert.Equal(t, len(publisher.PublishCalls()), 1)
	assert.DeepEqual(t, publisher.PublishCalls()[0].Event, &webhook.Event{
		URL:         "https://github.com/icholy/xagent/pull/83",
		Description: "A review comment was made on a pull request",
		Data:        "xagent: test comment",
	})
}
