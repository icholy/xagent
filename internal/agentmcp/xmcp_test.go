package agentmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/auth/agentauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
)

func setupTestSession(t *testing.T, srv *Server) *mcp.ClientSession {
	t.Helper()

	ctx := t.Context()

	// Create MCP server and add tools
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1.0.0"}, nil)
	srv.AddTools(mcpServer)

	// Create in-memory transports for server and client
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	// Connect server
	_, err := mcpServer.Connect(ctx, serverTransport, nil)
	assert.NilError(t, err)

	// Create and connect client
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1.0.0"}, nil)
	clientSession, err := mcpClient.Connect(t.Context(), clientTransport, nil)
	assert.NilError(t, err)
	t.Cleanup(func() { clientSession.Close() })

	return clientSession
}

func TestGetMyTask(t *testing.T) {
	client := &xagentclient.ClientMock{
		GetTaskDetailsFunc: func(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
			assert.Equal(t, req.Id, int64(123))
			return &xagentv1.GetTaskDetailsResponse{
				Task: &xagentv1.Task{
					Id:   123,
					Name: "test task",
				},
				Events: []*xagentv1.Event{
					{
						Payload: &xagentv1.Event_Instruction{
							Instruction: &xagentv1.InstructionPayload{
								Text: "do something",
								Url:  "https://example.com",
							},
						},
					},
				},
			}, nil
		},
	}

	srv := NewServer(client, &model.Task{
		ID:        123,
		Runner:    "test-runner",
		Workspace: "test-workspace",
	}, nil)
	session := setupTestSession(t, srv)

	// Call the tool through the MCP framework
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "get_my_task",
		Arguments: map[string]any{},
	})
	assert.NilError(t, err)

	assertTextResult(t, result, map[string]any{
		"id":        float64(123),
		"name":      "test task",
		"status":    "UNSPECIFIED",
		"workspace": "",
		"url":       "",
		"instructions": []any{
			map[string]any{"text": "do something", "url": "https://example.com"},
		},
		"links": []any{},
		"events": []any{
			map[string]any{
				"instruction": map[string]any{"text": "do something", "url": "https://example.com"},
			},
		},
	})
}

func TestGetGitHubToken(t *testing.T) {
	expiresAt := time.Date(2026, 5, 23, 1, 0, 0, 0, time.UTC)
	client := &xagentclient.ClientMock{
		CreateGitHubTokenFunc: func(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
			return &xagentv1.CreateGitHubTokenResponse{
				Token:     "ghs_test_token",
				ExpiresAt: timestamppb.New(expiresAt),
			}, nil
		},
	}

	srv := NewServer(client, &model.Task{ID: 123, Runner: "test-runner", Workspace: "test-workspace"}, []string{agentauth.CapabilityGitHubToken})
	session := setupTestSession(t, srv)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "get_github_token",
		Arguments: map[string]any{},
	})
	assert.NilError(t, err)

	assertTextResult(t, result, map[string]any{
		"token":     "ghs_test_token",
		"expiresAt": "2026-05-23T01:00:00Z",
	})

	assert.Equal(t, len(client.CreateGitHubTokenCalls()), 1)
}

func TestGetGitHubToken_Error(t *testing.T) {
	client := &xagentclient.ClientMock{
		CreateGitHubTokenFunc: func(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
			return nil, errors.New("no installation linked")
		},
	}

	srv := NewServer(client, &model.Task{ID: 123, Runner: "test-runner", Workspace: "test-workspace"}, []string{agentauth.CapabilityGitHubToken})
	session := setupTestSession(t, srv)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "get_github_token",
		Arguments: map[string]any{},
	})
	assert.NilError(t, err)
	assert.Assert(t, result.IsError, "expected error result")

	text, ok := result.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok, "expected TextContent")
	assert.Assert(t, strings.Contains(text.Text, "no installation linked"), "expected error message, got: %s", text.Text)
}

func TestGetGitHubToken_NotRegisteredWithoutCapability(t *testing.T) {
	client := &xagentclient.ClientMock{
		CreateGitHubTokenFunc: func(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
			t.Fatal("tool must not be callable when the capability is absent")
			return nil, nil
		},
	}

	srv := NewServer(client, &model.Task{ID: 123, Runner: "test-runner", Workspace: "test-workspace"}, nil)
	session := setupTestSession(t, srv)

	tools, err := session.ListTools(t.Context(), nil)
	assert.NilError(t, err)
	for _, tool := range tools.Tools {
		assert.Assert(t, tool.Name != "get_github_token", "get_github_token should not be registered without the capability")
	}
}

func assertTextResult(t *testing.T, result *mcp.CallToolResult, want map[string]any) {
	t.Helper()
	assert.Assert(t, result != nil, "CallTool returned nil result")
	assert.Assert(t, !result.IsError, "result is error: %v", result.Content)
	assert.Equal(t, len(result.Content), 1, "expected 1 content block, got %d", len(result.Content))
	text, ok := result.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok, "expected TextContent, got %T", result.Content[0])
	var got map[string]any
	assert.NilError(t, json.Unmarshal([]byte(text.Text), &got))
	assert.DeepEqual(t, got, want)
}
