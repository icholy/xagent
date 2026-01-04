package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/andygrunwald/go-jira/v2/cloud"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type JiraWebhookEvent struct {
	WebhookEvent string        `json:"webhookEvent"`
	Issue        *cloud.Issue  `json:"issue"`
	Comment      *cloud.Comment `json:"comment"`
	User         *cloud.User   `json:"user"`
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
	jiraURL   string
)

func init() {
	queueURL = os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		log.Fatal("SQS_QUEUE_URL environment variable is required")
	}

	secret = os.Getenv("JIRA_WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("JIRA_WEBHOOK_SECRET environment variable is required")
	}

	jiraURL = os.Getenv("JIRA_BASE_URL")
	if jiraURL == "" {
		log.Fatal("JIRA_BASE_URL environment variable is required")
	}

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	sqsClient = sqs.NewFromConfig(cfg)
}

func verifySignature(payload []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedMAC := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expectedMAC), []byte(signature))
}

func handleRequest(ctx context.Context, request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Verify webhook signature (Jira uses X-Hub-Signature header)
	signature := request.Headers["x-hub-signature"]
	if signature == "" {
		signature = request.Headers["X-Hub-Signature"]
	}

	if signature != "" && !verifySignature([]byte(request.Body), signature) {
		log.Printf("invalid signature")
		return events.APIGatewayProxyResponse{
			StatusCode: 401,
			Body:       `{"error": "invalid signature"}`,
		}, nil
	}

	// Parse webhook event
	var webhookEvent JiraWebhookEvent
	if err := json.Unmarshal([]byte(request.Body), &webhookEvent); err != nil {
		log.Printf("failed to parse webhook event: %v", err)
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Body:       fmt.Sprintf(`{"error": "invalid JSON: %v"}`, err),
		}, nil
	}

	// Only process comment_created events
	if webhookEvent.WebhookEvent != "comment_created" {
		log.Printf("ignoring event type: %s", webhookEvent.WebhookEvent)
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Body:       `{"status": "ignored"}`,
		}, nil
	}

	// Extract comment and issue information
	if webhookEvent.Comment == nil || webhookEvent.Issue == nil {
		log.Printf("missing comment or issue in webhook event")
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Body:       `{"error": "missing comment or issue"}`,
		}, nil
	}

	body := strings.TrimSpace(webhookEvent.Comment.Body)

	// Only process comments that start with "xagent task" or "xagent new"
	if !strings.HasPrefix(body, "xagent task") && !strings.HasPrefix(body, "xagent new") {
		log.Printf("ignoring comment that doesn't start with xagent command")
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Body:       `{"status": "ignored"}`,
		}, nil
	}

	// Construct issue URL
	issueURL := fmt.Sprintf("%s/browse/%s", strings.TrimSuffix(jiraURL, "/"), webhookEvent.Issue.Key)

	xagentEvent := &XAgentEvent{
		Description: body,
		Data:        request.Body,
		URL:         issueURL,
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
