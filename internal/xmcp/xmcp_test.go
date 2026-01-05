package xmcp

import (
	"context"
	"encoding/json"
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gotest.tools/v3/assert"
)

func TestGetMyTask(t *testing.T) {
	xagentClient := &ClientMock{
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

	// Create MCP server and add tools
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1.0.0"}, nil)
	xmcpServer := NewServer(xagentClient, 123, "test-workspace")
	xmcpServer.AddTools(mcpServer)

	// Create in-memory transports for server and client
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	// Connect server
	_, err := mcpServer.Connect(context.Background(), serverTransport, nil)
	assert.NilError(t, err)

	// Create and connect client
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1.0.0"}, nil)
	clientSession, err := mcpClient.Connect(context.Background(), clientTransport, nil)
	assert.NilError(t, err)
	defer clientSession.Close()

	// Call the tool through the MCP framework
	result, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_my_task",
		Arguments: map[string]any{},
	})

	assertTextResult(t, result, err, map[string]any{
		"name": "test task",
		"instructions": []any{
			map[string]any{"text": "do something", "url": "https://example.com"},
		},
		"links":    []any{},
		"events":   []any{},
		"children": []any{},
	})
}

func TestToolSchemas(t *testing.T) {
	xagentClient := &ClientMock{}

	// Create MCP server and add tools
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v1.0.0"}, nil)
	xmcpServer := NewServer(xagentClient, 123, "test-workspace")
	xmcpServer.AddTools(mcpServer)

	// Create in-memory transports for server and client
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	// Connect server
	_, err := mcpServer.Connect(context.Background(), serverTransport, nil)
	assert.NilError(t, err)

	// Create and connect client
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1.0.0"}, nil)
	clientSession, err := mcpClient.Connect(context.Background(), clientTransport, nil)
	assert.NilError(t, err)
	defer clientSession.Close()

	// List tools to verify they're registered with proper schemas
	tools, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{})
	assert.NilError(t, err)

	// Verify we have all expected tools
	expectedTools := map[string]bool{
		"create_link":          false,
		"report":               false,
		"get_my_task":          false,
		"create_child_task":    false,
		"update_my_task":       false,
		"list_child_tasks":     false,
		"update_child_task":    false,
		"list_child_task_logs": false,
	}

	for _, tool := range tools.Tools {
		if _, ok := expectedTools[tool.Name]; ok {
			expectedTools[tool.Name] = true
			// Verify that tools with input have non-nil input schemas
			if tool.Name != "get_my_task" && tool.Name != "list_child_tasks" {
				assert.Assert(t, tool.InputSchema != nil, "tool %s should have input schema", tool.Name)
			}
		}
	}

	for name, found := range expectedTools {
		assert.Assert(t, found, "tool %s not found in tool list", name)
	}
}

func assertTextResult(t *testing.T, result *mcp.CallToolResult, err error, want map[string]any) {
	t.Helper()
	assert.NilError(t, err, "CallTool returned error")
	assert.Assert(t, result != nil, "CallTool returned nil result")
	assert.Assert(t, !result.IsError, "result is error: %v", result.Content)
	assert.Equal(t, len(result.Content), 1, "expected 1 content block, got %d", len(result.Content))
	text, ok := result.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok, "expected TextContent, got %T", result.Content[0])
	var got map[string]any
	assert.NilError(t, json.Unmarshal([]byte(text.Text), &got))
	assert.DeepEqual(t, got, want)
}
