package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type pingInput struct {
	Message string `json:"message,omitempty" jsonschema:"Optional message to echo back"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "dummymcp",
		Version: "1.0.0",
	}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ping",
		Description: "A test tool that returns pong",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input pingInput) (*mcp.CallToolResult, any, error) {
		msg := input.Message
		if msg == "" {
			msg = "pong"
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: msg},
			},
		}, nil, nil
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
