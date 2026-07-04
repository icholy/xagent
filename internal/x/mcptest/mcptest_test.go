package mcptest_test

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gotest.tools/v3/assert"

	"github.com/icholy/xagent/internal/x/mcptest"
)

func TestUnmarshalCallToolResult(t *testing.T) {
	t.Parallel()
	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: `{"name":"widget","count":3}`}},
	}
	var got struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	mcptest.UnmarshalCallToolResult(t, res, &got)
	assert.Equal(t, got.Name, "widget")
	assert.Equal(t, got.Count, 3)
}
