package command

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/icholy/xagent/internal/mcpswap"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"
)

var GitHubMCPCommand = &cli.Command{
	Name:  "github-mcp",
	Usage: "Front the GitHub MCP server with rotating GitHub App installation tokens",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Usage:   "C2 server URL",
			Value:   xagentclient.DefaultURL,
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
		&cli.StringFlag{
			Name:    "token",
			Usage:   "Authentication token",
			Sources: cli.EnvVars("XAGENT_TOKEN"),
		},
		&cli.StringFlag{
			Name:  "url",
			Usage: "Upstream GitHub MCP endpoint",
			Value: "https://api.githubcopilot.com/mcp/",
		},
		&cli.DurationFlag{
			Name:  "refresh-margin",
			Usage: "How long before expiry to rotate the upstream session",
			Value: 5 * time.Minute,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

		client := xagentclient.New(xagentclient.Options{
			BaseURL: cmd.String("server"),
			Token:   cmd.String("token"),
		})
		url := cmd.String("url")
		margin := cmd.Duration("refresh-margin")

		var up mcpswap.Upstream
		up.SetLogger(logger)
		defer up.Close()

		swap := func(ctx context.Context) (time.Time, error) {
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

		expiresAt, err := swap(ctx)
		if err != nil {
			return err
		}

		go rotate(ctx, logger, swap, expiresAt, margin)

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
	},
}

// rotate refreshes the upstream session before each token's expiry. On
// failure it retries with a short backoff while the previous session keeps
// serving requests (Swap leaves the active session intact on error).
func rotate(ctx context.Context, logger *slog.Logger, swap func(context.Context) (time.Time, error), expiresAt time.Time, margin time.Duration) {
	const retryBackoff = 30 * time.Second
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

// bearerTransport injects an Authorization: Bearer header on every request.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}
