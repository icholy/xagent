package main

import (
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"
	"github.com/icholy/xagent/internal/webhook"
	"github.com/icholy/xagent/internal/xagentclient"
)

func mustEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		slog.Error("environment variable is required", "name", name)
		os.Exit(1)
	}
	return value
}

func main() {
	serverURL := mustEnv("XAGENT_SERVER")
	token := mustEnv("XAGENT_TOKEN")

	client := xagentclient.New(serverURL, xagentclient.StaticToken(token))

	publisher := &webhook.RPCPublisher{
		Client: client,
	}

	handler := webhook.NewHandler(&webhook.Config{
		GitHubSecret: mustEnv("GITHUB_WEBHOOK_SECRET"),
		JiraSecret:   mustEnv("JIRA_WEBHOOK_SECRET"),
		JiraBaseURL:  mustEnv("JIRA_BASE_URL"),
		Publisher:    publisher,
	})

	lambda.Start(httpadapter.NewV2(handler).ProxyWithContext)
}
