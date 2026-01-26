package xmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/icholy/xagent/internal/agentauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gotest.tools/v3/assert"
)

func setupTestSession(t *testing.T, srv *Server) *mcp.ClientSession {
	t.Helper()

	// Create MCP server and add tools
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1.0.0"}, nil)
	srv.AddTools(mcpServer)

	// Create in-memory transports for server and client
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	// Connect server
	_, err := mcpServer.Connect(t.Context(), serverTransport, nil)
	assert.NilError(t, err)

	// Create and connect client
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1.0.0"}, nil)
	clientSession, err := mcpClient.Connect(t.Context(), clientTransport, nil)
	assert.NilError(t, err)
	t.Cleanup(func() { clientSession.Close() })

	return clientSession
}

func TestGetMyTask(t *testing.T) {
	client := &ClientMock{
		GetTaskDetailsFunc: func(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
			assert.Equal(t, req.Id, int64(123))
			return &xagentv1.GetTaskDetailsResponse{
				Task: &xagentv1.Task{
					Id:   123,
					Name: "test task",
					Instructions: []*xagentv1.Instruction{
						{Text: "do something", Url: "https://example.com"},
					},
				},
			}, nil
		},
	}

	srv := NewServer(client, 123, "test-runner", "test-workspace")
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
		"status":    "",
		"workspace": "",
		"instructions": []any{
			map[string]any{"text": "do something", "url": "https://example.com"},
		},
		"links":    []any{},
		"events":   []any{},
		"children": []any{},
	})
}

func TestAgentFilter_UpdateTask_ArchivedTask(t *testing.T) {
	parentTaskID := int64(123)
	childTaskID := int64(456)

	client := &ClientMock{
		GetTaskFunc: func(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			assert.Equal(t, req.Id, childTaskID)
			return &xagentv1.GetTaskResponse{
				Task: &xagentv1.Task{
					Id:     childTaskID,
					Parent: parentTaskID,
					Status: "archived",
				},
			}, nil
		},
		UpdateTaskFunc: func(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
			t.Error("UpdateTask should not be called for archived tasks")
			return nil, nil
		},
	}

	filter := NewAgentFilter(client)

	// Create context with claims for the parent task
	ctx := agentauth.ContextWithClaims(context.Background(), &agentauth.TaskClaims{
		TaskID:    parentTaskID,
		Workspace: "test-workspace",
		Runner:    "test-runner",
	})

	_, err := filter.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: childTaskID})
	assert.Assert(t, err != nil, "expected error")
	assert.Assert(t, strings.Contains(err.Error(), "cannot update archived task"), "expected archived error message, got: %s", err.Error())
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
