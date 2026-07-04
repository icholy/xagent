package mcpx_test

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/durationpb"
	"gotest.tools/v3/assert"

	"github.com/icholy/xagent/internal/x/mcpx"
)

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	assert.Equal(t, len(res.Content), 1)
	text, ok := res.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok, "expected *mcp.TextContent, got %T", res.Content[0])
	return text.Text
}

func TestErrorResult(t *testing.T) {
	t.Parallel()
	res := mcpx.ErrorResult("boom: %d", 42)
	assert.Assert(t, res.IsError)
	assert.Equal(t, textOf(t, res), "boom: 42")
}

func TestJSONResult(t *testing.T) {
	t.Parallel()
	res := mcpx.JSONResult(map[string]int{"count": 3})
	assert.Assert(t, !res.IsError)
	assert.Equal(t, textOf(t, res), "{\n  \"count\": 3\n}")
}

func TestJSONResultMarshalError(t *testing.T) {
	t.Parallel()
	// channels cannot be marshalled to JSON, so this exercises the fallback.
	res := mcpx.JSONResult(make(chan int))
	assert.Assert(t, res.IsError)
	assert.Assert(t, len(textOf(t, res)) > 0)
}

func TestProtoJSONResult(t *testing.T) {
	t.Parallel()
	res := mcpx.ProtoJSONResult(durationpb.New(0))
	assert.Assert(t, !res.IsError)
	assert.Equal(t, textOf(t, res), "\"0s\"")
}
