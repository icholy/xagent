// Package mcptest provides small, general-purpose helpers for tests that
// exercise MCP tool handlers, built on gotest.tools/v3/assert.
package mcptest

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gotest.tools/v3/assert"
)

// UnmarshalCallToolResult validates the standard single-text-content success
// envelope a tool handler returns and unmarshals the JSON text payload into
// out (mirroring json.Unmarshal). It fails the test if the result is an error
// result, does not carry exactly one text content, or the payload does not
// decode into out.
func UnmarshalCallToolResult(t testing.TB, res *mcp.CallToolResult, out any) {
	t.Helper()
	assert.Assert(t, !res.IsError, "unexpected error result: %v", res.Content)
	assert.Equal(t, len(res.Content), 1)
	text, ok := res.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok, "expected *mcp.TextContent, got %T", res.Content[0])
	assert.NilError(t, json.Unmarshal([]byte(text.Text), out))
}
