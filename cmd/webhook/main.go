package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"
	"github.com/icholy/xagent/internal/webhook"
)

func mustEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s environment variable is required", name)
	}
	return value
}

func main() {
	ctx := context.Background()

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	publisher := &webhook.SQSPublisher{
		Client:   sqs.NewFromConfig(awsCfg),
		QueueURL: mustEnv("SQS_QUEUE_URL"),
	}

	handler := webhook.NewHandler(&webhook.Config{
		GitHubSecret: mustEnv("GITHUB_WEBHOOK_SECRET"),
		JiraSecret:   mustEnv("JIRA_WEBHOOK_SECRET"),
		JiraBaseURL:  mustEnv("JIRA_BASE_URL"),
		Publisher:    publisher,
	})

	lambda.Start(httpadapter.NewV2(handler).ProxyWithContext)
}
