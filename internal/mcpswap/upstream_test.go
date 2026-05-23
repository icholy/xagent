package mcpswap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// startUpstream returns an httptest server hosting an mcp.Server with a
// single "ping" tool that echoes "pong".
func startUpstream(t *testing.T) string {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake-upstream", Version: "0"}, nil)
	srv.AddTool(&mcp.Tool{
		Name:        "ping",
		Description: "returns pong",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil
	})
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	t.Cleanup(ts.Close)
	return ts.URL
}

// startProxy boots a Proxy pointing at upstreamURL and serves it via an
// httptest server, returning the proxy's URL.
func startProxy(t *testing.T, upstreamURL string) string {
	t.Helper()
	var up Upstream
	t.Cleanup(up.Close)
	if err := up.Swap(t.Context(), &mcp.StreamableClientTransport{Endpoint: upstreamURL}); err != nil {
		t.Fatalf("swap: %v", err)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "mcproxy", Version: "0"}, &mcp.ServerOptions{
		HasTools:     true,
		HasPrompts:   true,
		HasResources: true,
	})
	srv.AddReceivingMiddleware(up.Dispatch)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestUpstream_Dispatch(t *testing.T) {
	proxyURL := startProxy(t, startUpstream(t))

	c := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0"}, nil)
	sess, err := c.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: proxyURL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer sess.Close()

	// Capabilities mirror the upstream: it has a tool but no prompts or
	// resources, so the proxy must advertise tools only.
	caps := sess.InitializeResult().Capabilities
	if caps.Tools == nil {
		t.Error("proxy did not advertise tools capability")
	}
	if caps.Tools != nil && caps.Tools.ListChanged {
		t.Error("proxy advertised tools.listChanged it does not forward")
	}
	if caps.Prompts != nil {
		t.Error("proxy advertised prompts capability the upstream lacks")
	}
	if caps.Resources != nil {
		t.Error("proxy advertised resources capability the upstream lacks")
	}

	// Tool names pass through unprefixed.
	tools, err := sess.ListTools(t.Context(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools.Tools))
	}
	if tools.Tools[0].Name != "ping" {
		t.Fatalf("tool name = %q, want %q", tools.Tools[0].Name, "ping")
	}

	// Calls forward and return the upstream result.
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{Name: "ping"})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want *mcp.TextContent", res.Content[0])
	}
	if text.Text != "pong" {
		t.Fatalf("text = %q, want %q", text.Text, "pong")
	}
}

func TestUpstream_SessionWhenUnconnected(t *testing.T) {
	var up Upstream
	if _, err := up.Session(); err == nil {
		t.Fatal("expected error from Session before Swap")
	}
}
