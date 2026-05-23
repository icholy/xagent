// Package githubmcp adapts GitHub's MCP server for use with rotating
// GitHub App installation tokens. It holds a single upstream session to
// the GitHub MCP endpoint via internal/mcpswap and refreshes the
// underlying installation token before each expiry so a long-running
// agent never sees the 1h TTL break its session.
package githubmcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/icholy/xagent/internal/mcpswap"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DefaultURL is the default upstream GitHub MCP endpoint.
const DefaultURL = "https://api.githubcopilot.com/mcp/"

// DefaultRefreshMargin is the default time before token expiry at which
// the upstream session is rotated.
const DefaultRefreshMargin = 5 * time.Minute

// retryBackoff is how long to wait before retrying a failed token swap.
// The previous session keeps serving requests while we retry.
const retryBackoff = 30 * time.Second

// Config configures Run.
type Config struct {
	// Client is the xagent API client used to fetch GitHub App
	// installation tokens via CreateGitHubToken.
	Client xagentclient.Client
	// URL is the upstream GitHub MCP endpoint. Defaults to DefaultURL.
	URL string
	// RefreshMargin is how long before expires_at to rotate the upstream
	// session. Defaults to DefaultRefreshMargin.
	RefreshMargin time.Duration
	// Logger is used for lifecycle events. Defaults to slog.Default().
	// It must not write to stdout — stdout is the MCP stdio channel.
	Logger *slog.Logger
}

// Run fetches an initial installation token, opens an upstream session to
// the GitHub MCP server with it, and serves the proxied MCP server over
// stdio. A background goroutine refreshes the token before each expiry;
// swap failures are logged and retried while the previous session keeps
// serving. Run blocks until ctx is done or the MCP server returns.
//
// The initial swap must succeed — without a valid token the agent has
// nothing to forward to.
func Run(ctx context.Context, cfg Config) error {
	if cfg.URL == "" {
		cfg.URL = DefaultURL
	}
	if cfg.RefreshMargin == 0 {
		cfg.RefreshMargin = DefaultRefreshMargin
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	var up mcpswap.Upstream
	up.SetLogger(logger)
	defer up.Close()

	swap := func(ctx context.Context) (time.Time, error) {
		return swapUpstream(ctx, cfg.Client, &up, cfg.URL)
	}
	expiresAt, err := swap(ctx)
	if err != nil {
		return err
	}

	go rotate(ctx, logger, swap, expiresAt, cfg.RefreshMargin)

	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "xagent-github-mcp",
		Version: "1.0.0",
	}, &mcp.ServerOptions{
		HasTools:     true,
		HasPrompts:   true,
		HasResources: true,
	})
	srv.AddReceivingMiddleware(up.Dispatch)
	return srv.Run(ctx, &mcp.StdioTransport{})
}

// swapUpstream mints a fresh installation token and replaces the active
// upstream session with one connected using that token. Returns the new
// token's expiry.
func swapUpstream(ctx context.Context, client xagentclient.Client, up *mcpswap.Upstream, url string) (time.Time, error) {
	resp, err := client.CreateGitHubToken(ctx, &xagentv1.CreateGitHubTokenRequest{})
	if err != nil {
		return time.Time{}, fmt.Errorf("create github token: %w", err)
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint: url,
		HTTPClient: &http.Client{
			Transport: &bearerTransport{token: resp.Token, base: http.DefaultTransport},
		},
	}
	if err := up.Swap(ctx, transport); err != nil {
		return time.Time{}, fmt.Errorf("swap upstream: %w", err)
	}
	return resp.GetExpiresAt().AsTime(), nil
}

// rotate refreshes the upstream session before each token's expiry. On
// failure it retries with a short backoff while the previous session
// keeps serving requests (Swap leaves the active session intact on
// error). Returns when ctx is done.
func rotate(ctx context.Context, logger *slog.Logger, swap func(context.Context) (time.Time, error), expiresAt time.Time, margin time.Duration) {
	for {
		wait := time.Until(expiresAt.Add(-margin))
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		next, err := swap(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Error("github-mcp: token rotation failed; retrying", "err", err, "backoff", retryBackoff)
			expiresAt = time.Now().Add(retryBackoff + margin)
			continue
		}
		expiresAt = next
	}
}

// bearerTransport injects an Authorization: Bearer header on every
// request. A fresh transport is built per swap so a session always sees
// the token it was opened with.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}
