package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"
	"github.com/icholy/xagent/internal/webhook"
)

type sqsPublisher struct {
	client   *sqs.Client
	queueURL string
}

func (p *sqsPublisher) Publish(event *webhook.Event) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	_, err = p.client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    &p.queueURL,
		MessageBody: aws.String(string(eventJSON)),
	})
	if err != nil {
		return fmt.Errorf("failed to send message to SQS: %w", err)
	}

	return nil
}

func main() {
	ctx := context.Background()

	queueURL := os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		log.Fatal("SQS_QUEUE_URL environment variable is required")
	}

	githubSecret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if githubSecret == "" {
		log.Fatal("GITHUB_WEBHOOK_SECRET environment variable is required")
	}

	jiraSecret := os.Getenv("JIRA_WEBHOOK_SECRET")
	if jiraSecret == "" {
		log.Fatal("JIRA_WEBHOOK_SECRET environment variable is required")
	}

	jiraBaseURL := os.Getenv("JIRA_BASE_URL")
	if jiraBaseURL == "" {
		log.Fatal("JIRA_BASE_URL environment variable is required")
	}

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	publisher := &sqsPublisher{
		client:   sqs.NewFromConfig(awsCfg),
		queueURL: queueURL,
	}

	handler := webhook.NewHandler(&webhook.Config{
		GitHubSecret: githubSecret,
		JiraSecret:   jiraSecret,
		JiraBaseURL:  jiraBaseURL,
		Publisher:    publisher,
	})

	lambda.Start(httpadapter.New(handler).ProxyWithContext)
}
