package agentmcp

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ChannelNotifier sends notifications/claude/channel events to Claude Code.
// It wraps a stdio transport to capture the underlying connection.
type ChannelNotifier struct {
	conn mcp.Connection
}

// channelTransport wraps an MCP transport to capture the connection.
type channelTransport struct {
	inner    mcp.Transport
	notifier *ChannelNotifier
}

func (t *channelTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	t.notifier.conn = conn
	return conn, nil
}

// WrapTransport wraps a transport so the returned ChannelNotifier can send
// channel notifications after the connection is established.
func WrapTransport(t mcp.Transport) (mcp.Transport, *ChannelNotifier) {
	n := &ChannelNotifier{}
	return &channelTransport{inner: t, notifier: n}, n
}

// ChannelParams is the notification payload for notifications/claude/channel.
type ChannelParams struct {
	Content string            `json:"content"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// Notify sends a channel notification to Claude Code.
func (n *ChannelNotifier) Notify(ctx context.Context, content string, meta map[string]string) error {
	if n.conn == nil {
		return nil
	}
	params := ChannelParams{
		Content: content,
		Meta:    meta,
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	msg := &jsonrpc.Request{
		Method: "notifications/claude/channel",
		Params: raw,
	}
	return n.conn.Write(ctx, msg)
}
