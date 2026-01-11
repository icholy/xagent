package webhook

//go:generate go tool moq -pkg webhook_test -out publisher_moq_test.go . Publisher

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/andygrunwald/go-jira/v2/cloud"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/go-github/v68/github"
	"github.com/icholy/xagent/internal/githubx"
)

// Event represents an xagent webhook event.
type Event struct {
	Description string `json:"description"`
	Data        string `json:"data"`
	URL         string `json:"url"`
}

// Publisher is the interface for publishing webhook events.
type Publisher interface {
	Publish(event *Event) error
}

// SQSPublisher publishes events to an SQS queue.
type SQSPublisher struct {
	Client   *sqs.Client
	QueueURL string
}

// Publish sends an event to the SQS queue.
func (p *SQSPublisher) Publish(event *Event) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	_, err = p.Client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    &p.QueueURL,
		MessageBody: aws.String(string(eventJSON)),
	})
	if err != nil {
		return fmt.Errorf("failed to send message to SQS: %w", err)
	}

	return nil
}

// Config holds the webhook handler configuration.
type Config struct {
	GitHubSecret string
	JiraSecret   string
	JiraBaseURL  string
	Publisher    Publisher
	NoVerify     bool
}

// Handler is an http.Handler that processes GitHub and Jira webhooks.
type Handler struct {
	config *Config
	mux    *http.ServeMux
}

// NewHandler creates a new webhook handler.
func NewHandler(config *Config) *Handler {
	h := &Handler{config: config}
	h.mux = http.NewServeMux()
	h.mux.HandleFunc("/webhook/github", h.handleGitHub)
	h.mux.HandleFunc("/webhook/jira", h.handleJira)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) handleGitHub(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read body", "error", err)
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	var secret []byte
	if !h.config.NoVerify {
		secret = []byte(h.config.GitHubSecret)
	}

	webhookEvent, err := githubx.ParseWebHook(body, r.Header, secret)
	if err != nil {
		slog.Error("failed to parse GitHub webhook", "error", err)
		http.Error(w, "Invalid webhook", http.StatusBadRequest)
		return
	}

	event := h.extractGitHubEvent(webhookEvent)

	if event == nil {
		eventType := r.Header.Get("X-GitHub-Event")
		slog.Debug("ignoring GitHub event type", "event_type", eventType)
		fmt.Fprintf(w, "ignored GitHub event type: %s", eventType)
		return
	}

	if err := h.config.Publisher.Publish(event); err != nil {
		slog.Error("failed to publish event", "error", err)
		http.Error(w, "Failed to publish event", http.StatusInternalServerError)
		return
	}

	slog.Info("GitHub event published", "url", event.URL)
	fmt.Fprintf(w, "processed GitHub event: %s", event.URL)
}

func (h *Handler) handleJira(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read body", "error", err)
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	signature := r.Header.Get("X-Hub-Signature")
	if signature != "" && !h.verifyJiraSignature(body, signature) {
		slog.Warn("invalid Jira signature")
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	var webhookEvent jiraWebhookEvent
	if err := json.Unmarshal(body, &webhookEvent); err != nil {
		slog.Error("failed to parse Jira webhook", "error", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	event := h.extractJiraEvent(&webhookEvent, string(body))

	if event == nil {
		slog.Debug("ignoring Jira event type", "event_type", webhookEvent.WebhookEvent)
		fmt.Fprintf(w, "ignored Jira event type: %s", webhookEvent.WebhookEvent)
		return
	}

	if err := h.config.Publisher.Publish(event); err != nil {
		slog.Error("failed to publish event", "error", err)
		http.Error(w, "Failed to publish event", http.StatusInternalServerError)
		return
	}

	slog.Info("Jira event published", "url", event.URL)
	fmt.Fprintf(w, "processed Jira event: %s", event.URL)
}

func (h *Handler) verifyJiraSignature(payload []byte, signature string) bool {
	if h.config.NoVerify {
		return true
	}
	mac := hmac.New(sha256.New, []byte(h.config.JiraSecret))
	mac.Write(payload)
	expectedMAC := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expectedMAC), []byte(signature))
}

func (h *Handler) extractGitHubEvent(webhookEvent any) *Event {
	switch event := webhookEvent.(type) {
	case *github.IssueCommentEvent:
		if event.Comment != nil && event.Issue != nil &&
			event.Comment.Body != nil && event.Issue.HTMLURL != nil {
			body := strings.TrimSpace(*event.Comment.Body)
			if strings.HasPrefix(body, "xagent:") {
				description := "A comment was made on an issue"
				if event.Issue.IsPullRequest() {
					description = "A comment was made on a pull request"
				}
				return &Event{
					Description: description,
					Data:        body,
					URL:         *event.Issue.HTMLURL,
				}
			}
		}

	case *github.PullRequestReviewCommentEvent:
		if event.Comment != nil && event.PullRequest != nil &&
			event.Comment.Body != nil && event.PullRequest.HTMLURL != nil {
			body := strings.TrimSpace(*event.Comment.Body)
			if strings.HasPrefix(body, "xagent:") {
				return &Event{
					Description: "A review comment was made on a pull request",
					Data:        body,
					URL:         *event.PullRequest.HTMLURL,
				}
			}
		}
	}

	return nil
}

type jiraWebhookEvent struct {
	WebhookEvent string         `json:"webhookEvent"`
	Issue        *cloud.Issue   `json:"issue"`
	Comment      *cloud.Comment `json:"comment"`
	User         *cloud.User    `json:"user"`
}

func (h *Handler) extractJiraEvent(event *jiraWebhookEvent, rawBody string) *Event {
	if event.WebhookEvent != "comment_created" {
		return nil
	}

	if event.Comment == nil || event.Issue == nil {
		return nil
	}

	body := strings.TrimSpace(event.Comment.Body)
	if !strings.HasPrefix(body, "xagent:") {
		return nil
	}

	issueURL := fmt.Sprintf("%s/browse/%s", strings.TrimSuffix(h.config.JiraBaseURL, "/"), event.Issue.Key)

	return &Event{
		Description: "A comment was made on a Jira issue",
		Data:        body,
		URL:         issueURL,
	}
}
