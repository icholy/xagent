package agentmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gotest.tools/v3/assert"
)

func TestWrapTransport(t *testing.T) {
	inner := &mcp.InMemoryTransport{}
	wrapped, notifier := WrapTransport(inner)
	assert.Assert(t, wrapped != nil)
	assert.Assert(t, notifier != nil)
	assert.Assert(t, notifier.conn == nil, "conn should be nil before Connect")
}

func TestChannelNotifier_NotifyBeforeConnect(t *testing.T) {
	n := &ChannelNotifier{}
	// Should not error when conn is nil
	err := n.Notify(context.Background(), "test", nil)
	assert.NilError(t, err)
}

// mockConn implements mcp.Connection for testing
type mockConn struct {
	messages []jsonrpc.Message
}

func (m *mockConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *mockConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockConn) Close() error     { return nil }
func (m *mockConn) SessionID() string { return "test" }

func TestChannelNotifier_Notify(t *testing.T) {
	conn := &mockConn{}
	n := &ChannelNotifier{conn: conn}

	err := n.Notify(context.Background(), "test content", map[string]string{
		"task_id":    "42",
		"event_type": "child_completed",
	})
	assert.NilError(t, err)
	assert.Equal(t, len(conn.messages), 1)

	msg, ok := conn.messages[0].(*jsonrpc.Request)
	assert.Assert(t, ok, "expected *jsonrpc.Request")
	assert.Equal(t, msg.Method, "notifications/claude/channel")
	assert.Assert(t, !msg.ID.IsValid(), "notifications should have no ID")

	var params ChannelParams
	err = json.Unmarshal(msg.Params, &params)
	assert.NilError(t, err)
	assert.Equal(t, params.Content, "test content")
	assert.Equal(t, params.Meta["task_id"], "42")
	assert.Equal(t, params.Meta["event_type"], "child_completed")
}

func TestChannelNotifier_NotifyNoMeta(t *testing.T) {
	conn := &mockConn{}
	n := &ChannelNotifier{conn: conn}

	err := n.Notify(context.Background(), "simple message", nil)
	assert.NilError(t, err)

	msg := conn.messages[0].(*jsonrpc.Request)
	var params ChannelParams
	err = json.Unmarshal(msg.Params, &params)
	assert.NilError(t, err)
	assert.Equal(t, params.Content, "simple message")
	assert.Assert(t, params.Meta == nil)
}

func TestChannelCapabilities(t *testing.T) {
	// Verify the channel instructions are set
	assert.Assert(t, ChannelInstructions != "")
	assert.Assert(t, len(ChannelInstructions) > 0)
}
