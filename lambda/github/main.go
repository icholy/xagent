package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/go-github/v68/github"
)

type GitHubWebhookEvent struct {
	Action      *string              `json:"action"`
	Issue       *github.Issue        `json:"issue"`
	PullRequest *github.PullRequest  `json:"pull_request"`
	Comment     *github.IssueComment `json:"comment"`
	Repository  *github.Repository   `json:"repository"`
	Sender      *github.User         `json:"sender"`
}

type XAgentEvent struct {
	Description string `json:"description"`
	Data        string `json:"data"`
	URL         string `json:"url"`
}

var (
	sqsClient *sqs.Client
	queueURL  string
	secret    string
)

func init() {
	queueURL = os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		log.Fatal("SQS_QUEUE_URL environment variable is required")
	}

	secret = os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("GITHUB_WEBHOOK_SECRET environment variable is required")
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	sqsClient = sqs.NewFromConfig(cfg)
}

func verifySignature(payload []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	expectedMAC := signature[7:]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	actualMAC := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(actualMAC), []byte(expectedMAC))
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Verify webhook signature
	signature := request.Headers["x-hub-signature-256"]
	if signature == "" {
		signature = request.Headers["X-Hub-Signature-256"]
	}

	if !verifySignature([]byte(request.Body), signature) {
		log.Printf("invalid signature")
		return events.APIGatewayProxyResponse{
			StatusCode: 401,
			Body:       `{"error": "invalid signature"}`,
		}, nil
	}

	// Parse webhook event
	var webhookEvent GitHubWebhookEvent
	if err := json.Unmarshal([]byte(request.Body), &webhookEvent); err != nil {
		log.Printf("failed to parse webhook event: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Body:       fmt.Sprintf(`{"error": "invalid JSON: %v"}`, err),
		}, nil
	}

	// Determine event type and extract relevant information
	eventType := request.Headers["x-github-event"]
	if eventType == "" {
		eventType = request.Headers["X-GitHub-Event"]
	}

	var xagentEvent *XAgentEvent

	switch eventType {
	case "issue_comment":
		if webhookEvent.Comment != nil && webhookEvent.Issue != nil &&
			webhookEvent.Comment.Body != nil && webhookEvent.Issue.HTMLURL != nil {
			body := strings.TrimSpace(*webhookEvent.Comment.Body)
			// Only process comments that start with "xagent task" or "xagent new"
			if strings.HasPrefix(body, "xagent task") || strings.HasPrefix(body, "xagent new") {
				xagentEvent = &XAgentEvent{
					Description: body,
					Data:        request.Body,
					URL:         *webhookEvent.Issue.HTMLURL,
				}
			}
		}

	case "pull_request_review_comment", "pull_request":
		if webhookEvent.Comment != nil && webhookEvent.PullRequest != nil &&
			webhookEvent.Comment.Body != nil && webhookEvent.PullRequest.HTMLURL != nil {
			body := strings.TrimSpace(*webhookEvent.Comment.Body)
			// Only process comments that start with "xagent task" or "xagent new"
			if strings.HasPrefix(body, "xagent task") || strings.HasPrefix(body, "xagent new") {
				xagentEvent = &XAgentEvent{
					Description: body,
					Data:        request.Body,
					URL:         *webhookEvent.PullRequest.HTMLURL,
				}
			}
		}
	}

	// If no event to process, return success
	if xagentEvent == nil {
		log.Printf("ignoring event type: %s", eventType)
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Body:       `{"status": "ignored"}`,
		}, nil
	}

	// Publish to SQS
	eventJSON, err := json.Marshal(xagentEvent)
	if err != nil {
		log.Printf("failed to marshal event: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Body:       fmt.Sprintf(`{"error": "failed to marshal event: %v"}`, err),
		}, nil
	}

	_, err = sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    &queueURL,
		MessageBody: stringPtr(string(eventJSON)),
	})
	if err != nil {
		log.Printf("failed to send message to SQS: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Body:       fmt.Sprintf(`{"error": "failed to send message: %v"}`, err),
		}, nil
	}

	log.Printf("event published to SQS: %s", xagentEvent.URL)
	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       `{"status": "processed"}`,
	}, nil
}

func stringPtr(s string) *string {
	return &s
}

func main() {
	lambda.Start(handleRequest)
}
