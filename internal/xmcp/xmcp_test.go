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

	srv := NewServer(client, 123, "test-workspace")
	result, _, err := srv.getMyTask(context.Background(), &mcp.CallToolRequest{}, nil)
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

func assertTextResult(t *testing.T, result *mcp.CallToolResult, err error, want map[string]any) {
	t.Helper()
	assert.NilError(t, err)
	assert.Assert(t, !result.IsError, "result is error: %v", result.Content)
	assert.Equal(t, len(result.Content), 1, "expected 1 content block, got %d", len(result.Content))
	text, ok := result.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok, "expected TextContent, got %T", result.Content[0])
	var got map[string]any
	assert.NilError(t, json.Unmarshal([]byte(text.Text), &got))
	assert.DeepEqual(t, got, want)
}
