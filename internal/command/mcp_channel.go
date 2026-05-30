package command

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// channelTransport wraps an mcp.Transport so the bridge can inject
// send-only JSON-RPC notifications (e.g. notifications/claude/channel)
// alongside the SDK's own writes on the same connection.
//
// TODO(go-sdk #745): replace with upstream ServerSession.Notify once the
// combined send/receive design lands.
// https://github.com/modelcontextprotocol/go-sdk/issues/745
type channelTransport struct {
	inner mcp.Transport

	mu   sync.Mutex
	conn mcp.Connection
}

func newChannelTransport(inner mcp.Transport) *channelTransport {
	return &channelTransport{inner: inner}
}

// Connect implements mcp.Transport. It delegates to the inner transport,
// retains the returned connection so Notify can write to it, and returns
// a wrapper connection that funnels writes through the same mutex.
func (t *channelTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	conn, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.conn = conn
	t.mu.Unlock()
	return &channelConn{t: t, inner: conn}, nil
}

// Notify writes a JSON-RPC notification (a Request with no ID) for the
// given method and params. It is safe to call concurrently with the
// SDK's own writes on the underlying connection.
func (t *channelTransport) Notify(ctx context.Context, method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal params: %w", err)
	}
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("channel transport not connected")
	}
	req := &jsonrpc.Request{Method: method, Params: raw}
	t.mu.Lock()
	defer t.mu.Unlock()
	return conn.Write(ctx, req)
}

// channelConn wraps an mcp.Connection and serializes writes through the
// parent transport's mutex so injected notification frames cannot
// interleave with the SDK's own writes.
type channelConn struct {
	t     *channelTransport
	inner mcp.Connection
}

func (c *channelConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	return c.inner.Read(ctx)
}

func (c *channelConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	c.t.mu.Lock()
	defer c.t.mu.Unlock()
	return c.inner.Write(ctx, msg)
}

func (c *channelConn) Close() error {
	return c.inner.Close()
}

func (c *channelConn) SessionID() string {
	return c.inner.SessionID()
}
