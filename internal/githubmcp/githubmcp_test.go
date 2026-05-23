package githubmcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/mcpswap"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gotest.tools/v3/assert"
)

// startAuthedUpstream stands up an MCP server behind an Authorization
// check. It records every Bearer token seen and rejects requests whose
// token isn't one of the accepted ones.
func startAuthedUpstream(t *testing.T, accepted map[string]bool, seen *atomic.Value) string {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake-github-mcp", Version: "0"}, nil)
	srv.AddTool(&mcp.Tool{
		Name:        "ping",
		Description: "returns pong",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil
	})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		seen.Store(token)
		if !accepted[token] {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestSwapUpstream_InjectsBearerToken(t *testing.T) {
	var seen atomic.Value
	url := startAuthedUpstream(t, map[string]bool{"ghs_test_token": true}, &seen)

	client := &xagentclient.ClientMock{
		CreateGitHubTokenFunc: func(_ context.Context, _ *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
			return &xagentv1.CreateGitHubTokenResponse{
				Token:     "ghs_test_token",
				ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)),
			}, nil
		},
	}

	var up mcpswap.Upstream
	t.Cleanup(up.Close)
	expiresAt, err := swapUpstream(t.Context(), client, &up, url)
	assert.NilError(t, err)
	assert.Assert(t, !expiresAt.IsZero(), "expiresAt should be set from the token response")
	assert.Equal(t, len(client.CreateGitHubTokenCalls()), 1)

	sess, err := up.Session()
	assert.NilError(t, err)
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{Name: "ping"})
	assert.NilError(t, err)
	text, ok := res.Content[0].(*mcp.TextContent)
	assert.Assert(t, ok)
	assert.Equal(t, text.Text, "pong")
	assert.Equal(t, seen.Load(), "ghs_test_token")
}

func TestSwapUpstream_PropagatesTokenError(t *testing.T) {
	client := &xagentclient.ClientMock{
		CreateGitHubTokenFunc: func(_ context.Context, _ *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
			return nil, errors.New("boom")
		},
	}
	var up mcpswap.Upstream
	t.Cleanup(up.Close)
	_, err := swapUpstream(t.Context(), client, &up, "http://invalid.invalid")
	assert.ErrorContains(t, err, "create github token")
}
