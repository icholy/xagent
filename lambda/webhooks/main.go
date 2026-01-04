package main

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
	"net/url"
	"os"
	"strings"

	"github.com/andygrunwald/go-jira/v2/cloud"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/go-github/v68/github"
)

type Config struct {
	SQSQueueURL       string
	GitHubSecret      string
	JiraSecret        string
	JiraBaseURL       string
	SQSClient         *sqs.Client
}

type Handler struct {
	config *Config
}

type XAgentEvent struct {
	Description string `json:"description"`
	Data        string `json:"data"`
	URL         string `json:"url"`
}

type GitHubWebhookEvent struct {
	Action      *string              `json:"action"`
	Issue       *github.Issue        `json:"issue"`
	PullRequest *github.PullRequest  `json:"pull_request"`
	Comment     *github.IssueComment `json:"comment"`
	Repository  *github.Repository   `json:"repository"`
	Sender      *github.User         `json:"sender"`
}

type JiraWebhookEvent struct {
	WebhookEvent string         `json:"webhookEvent"`
	Issue        *cloud.Issue   `json:"issue"`
	Comment      *cloud.Comment `json:"comment"`
	User         *cloud.User    `json:"user"`
}

func NewConfig(ctx context.Context) (*Config, error) {
	queueURL := os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		return nil, fmt.Errorf("SQS_QUEUE_URL environment variable is required")
	}

	githubSecret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if githubSecret == "" {
		return nil, fmt.Errorf("GITHUB_WEBHOOK_SECRET environment variable is required")
	}

	jiraSecret := os.Getenv("JIRA_WEBHOOK_SECRET")
	if jiraSecret == "" {
		return nil, fmt.Errorf("JIRA_WEBHOOK_SECRET environment variable is required")
	}

	jiraBaseURL := os.Getenv("JIRA_BASE_URL")
	if jiraBaseURL == "" {
		return nil, fmt.Errorf("JIRA_BASE_URL environment variable is required")
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &Config{
		SQSQueueURL:  queueURL,
		GitHubSecret: githubSecret,
		JiraSecret:   jiraSecret,
		JiraBaseURL:  jiraBaseURL,
		SQSClient:    sqs.NewFromConfig(cfg),
	}, nil
}

func NewHandler(config *Config) *Handler {
	return &Handler{config: config}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/webhook/github":
		h.handleGitHub(w, r)
	case "/webhook/jira":
		h.handleJira(w, r)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (h *Handler) handleGitHub(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("failed to read body: %v", err)
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Verify signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if !h.verifyGitHubSignature(body, signature) {
		log.Printf("invalid GitHub signature")
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Parse webhook event
	var webhookEvent GitHubWebhookEvent
	if err := json.Unmarshal(body, &webhookEvent); err != nil {
		log.Printf("failed to parse GitHub webhook: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	xagentEvent := h.extractGitHubEvent(&webhookEvent, eventType, string(body))

	if xagentEvent == nil {
		log.Printf("ignoring GitHub event type: %s", eventType)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ignored"})
		return
	}

	if err := h.publishToSQS(r.Context(), xagentEvent); err != nil {
		log.Printf("failed to publish to SQS: %v", err)
		http.Error(w, "Failed to publish event", http.StatusInternalServerError)
		return
	}

	log.Printf("GitHub event published to SQS: %s", xagentEvent.URL)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "processed"})
}

func (h *Handler) handleJira(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("failed to read body: %v", err)
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Verify signature if present
	signature := r.Header.Get("X-Hub-Signature")
	if signature != "" && !h.verifyJiraSignature(body, signature) {
		log.Printf("invalid Jira signature")
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Parse webhook event
	var webhookEvent JiraWebhookEvent
	if err := json.Unmarshal(body, &webhookEvent); err != nil {
		log.Printf("failed to parse Jira webhook: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	xagentEvent := h.extractJiraEvent(&webhookEvent, string(body))

	if xagentEvent == nil {
		log.Printf("ignoring Jira event type: %s", webhookEvent.WebhookEvent)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ignored"})
		return
	}

	if err := h.publishToSQS(r.Context(), xagentEvent); err != nil {
		log.Printf("failed to publish to SQS: %v", err)
		http.Error(w, "Failed to publish event", http.StatusInternalServerError)
		return
	}

	log.Printf("Jira event published to SQS: %s", xagentEvent.URL)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "processed"})
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

func (h *Handler) extractGitHubEvent(event *GitHubWebhookEvent, eventType, rawBody string) *XAgentEvent {
	switch eventType {
	case "issue_comment":
		if event.Comment != nil && event.Issue != nil &&
			event.Comment.Body != nil && event.Issue.HTMLURL != nil {
			body := strings.TrimSpace(*event.Comment.Body)
			if strings.HasPrefix(body, "xagent task") || strings.HasPrefix(body, "xagent new") {
				return &XAgentEvent{
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
				return &XAgentEvent{
					Description: body,
					Data:        rawBody,
					URL:         *event.PullRequest.HTMLURL,
				}
			}
		}
	}

	return nil
}

func (h *Handler) extractJiraEvent(event *JiraWebhookEvent, rawBody string) *XAgentEvent {
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

	return &XAgentEvent{
		Description: body,
		Data:        rawBody,
		URL:         issueURL,
	}
}

func (h *Handler) publishToSQS(ctx context.Context, event *XAgentEvent) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	queueURL := h.config.SQSQueueURL
	_, err = h.config.SQSClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    &queueURL,
		MessageBody: stringPtr(string(eventJSON)),
	})
	if err != nil {
		return fmt.Errorf("failed to send message to SQS: %w", err)
	}

	return nil
}

func stringPtr(s string) *string {
	return &s
}

// lambdaHandler adapts HTTP handler to Lambda events
func lambdaHandler(ctx context.Context, cfg *Config) func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	handler := NewHandler(cfg)

	return func(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
		// Convert API Gateway event to HTTP request
		req := &http.Request{
			Method: request.HTTPMethod,
			URL:    &url.URL{Path: request.Path},
			Header: make(http.Header),
			Body:   io.NopCloser(strings.NewReader(request.Body)),
		}

		// Copy headers (case-insensitive)
		for k, v := range request.Headers {
			req.Header.Set(k, v)
		}

		// Create response writer
		w := &responseWriter{
			statusCode: 200,
			headers:    make(http.Header),
		}

		// Handle request
		handler.ServeHTTP(w, req)

		// Convert response
		headers := make(map[string]string)
		for k, v := range w.headers {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}

		return events.APIGatewayProxyResponse{
			StatusCode: w.statusCode,
			Headers:    headers,
			Body:       w.body.String(),
		}, nil
	}
}

type responseWriter struct {
	statusCode int
	headers    http.Header
	body       strings.Builder
}

func (w *responseWriter) Header() http.Header {
	return w.headers
}

func (w *responseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *responseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func main() {
	ctx := context.Background()
	cfg, err := NewConfig(ctx)
	if err != nil {
		log.Fatalf("failed to initialize config: %v", err)
	}

	lambda.Start(lambdaHandler(ctx, cfg))
}
