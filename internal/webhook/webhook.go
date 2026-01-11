package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/andygrunwald/go-jira/v2/cloud"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/go-github/v68/github"
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
		log.Printf("failed to read body: %v", err)
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	signature := r.Header.Get("X-Hub-Signature-256")
	if !h.verifyGitHubSignature(body, signature) {
		log.Printf("invalid GitHub signature")
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	var webhookEvent gitHubWebhookEvent
	if err := json.Unmarshal(body, &webhookEvent); err != nil {
		log.Printf("failed to parse GitHub webhook: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	event := h.extractGitHubEvent(&webhookEvent, eventType, string(body))

	if event == nil {
		log.Printf("ignoring GitHub event type: %s", eventType)
		fmt.Fprintf(w, "ignored GitHub event type: %s", eventType)
		return
	}

	if err := h.config.Publisher.Publish(event); err != nil {
		log.Printf("failed to publish event: %v", err)
		http.Error(w, "Failed to publish event", http.StatusInternalServerError)
		return
	}

	log.Printf("GitHub event published: %s", event.URL)
	fmt.Fprintf(w, "processed GitHub event: %s", event.URL)
}

func (h *Handler) handleJira(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("failed to read body: %v", err)
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	signature := r.Header.Get("X-Hub-Signature")
	if signature != "" && !h.verifyJiraSignature(body, signature) {
		log.Printf("invalid Jira signature")
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	var webhookEvent jiraWebhookEvent
	if err := json.Unmarshal(body, &webhookEvent); err != nil {
		log.Printf("failed to parse Jira webhook: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	event := h.extractJiraEvent(&webhookEvent, string(body))

	if event == nil {
		log.Printf("ignoring Jira event type: %s", webhookEvent.WebhookEvent)
		fmt.Fprintf(w, "ignored Jira event type: %s", webhookEvent.WebhookEvent)
		return
	}

	if err := h.config.Publisher.Publish(event); err != nil {
		log.Printf("failed to publish event: %v", err)
		http.Error(w, "Failed to publish event", http.StatusInternalServerError)
		return
	}

	log.Printf("Jira event published: %s", event.URL)
	fmt.Fprintf(w, "processed Jira event: %s", event.URL)
}

func (h *Handler) verifyGitHubSignature(payload []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	expectedMAC := signature[7:]
	mac := hmac.New(sha256.New, []byte(h.config.GitHubSecret))
	mac.Write(payload)
	actualMAC := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(actualMAC), []byte(expectedMAC))
}

func (h *Handler) verifyJiraSignature(payload []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(h.config.JiraSecret))
	mac.Write(payload)
	expectedMAC := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expectedMAC), []byte(signature))
}

type gitHubWebhookEvent struct {
	Action      *string              `json:"action"`
	Issue       *github.Issue        `json:"issue"`
	PullRequest *github.PullRequest  `json:"pull_request"`
	Comment     *github.IssueComment `json:"comment"`
	Repository  *github.Repository   `json:"repository"`
	Sender      *github.User         `json:"sender"`
}

func (h *Handler) extractGitHubEvent(event *gitHubWebhookEvent, eventType, rawBody string) *Event {
	switch eventType {
	case "issue_comment":
		if event.Comment != nil && event.Issue != nil &&
			event.Comment.Body != nil && event.Issue.HTMLURL != nil {
			body := strings.TrimSpace(*event.Comment.Body)
			if strings.HasPrefix(body, "xagent task") || strings.HasPrefix(body, "xagent new") {
				return &Event{
					Description: body,
					Data:        rawBody,
					URL:         *event.Issue.HTMLURL,
				}
			}
		}

	case "pull_request_review_comment", "pull_request":
		if event.Comment != nil && event.PullRequest != nil &&
			event.Comment.Body != nil && event.PullRequest.HTMLURL != nil {
			body := strings.TrimSpace(*event.Comment.Body)
			if strings.HasPrefix(body, "xagent task") || strings.HasPrefix(body, "xagent new") {
				return &Event{
					Description: body,
					Data:        rawBody,
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
	if !strings.HasPrefix(body, "xagent task") && !strings.HasPrefix(body, "xagent new") {
		return nil
	}

	issueURL := fmt.Sprintf("%s/browse/%s", strings.TrimSuffix(h.config.JiraBaseURL, "/"), event.Issue.Key)

	return &Event{
		Description: body,
		Data:        rawBody,
		URL:         issueURL,
	}
}
