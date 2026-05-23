// Package mcpswap is a single-upstream MCP adapter. It holds a live
// session to one upstream MCP server and forwards requests to it via
// Upstream.Dispatch, an mcp receiving middleware. The active session
// can be hot-swapped at any time with Upstream.Swap, letting the caller
// rotate credentials without dropping in-flight requests. Transport
// construction and rotation policy live with the caller.
package mcpswap

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Upstream holds the currently-active session to the upstream MCP
// server. Swap connects a new session and atomically replaces the
// active one; readers see either the old or the new session, never a
// torn state. Upstream knows nothing about credentials or rotation —
// the caller decides when to Swap and with what config.
//
// The zero value is ready to use; it logs to slog.Default() until
// SetLogger is called.
type Upstream struct {
	logger  atomic.Pointer[slog.Logger]
	swapMu  sync.Mutex
	session atomic.Pointer[mcp.ClientSession]
}

// SetLogger sets the logger used for lifecycle events. It may be called
// at any time.
func (u *Upstream) SetLogger(logger *slog.Logger) {
	u.logger.Store(logger)
}

// log returns the configured logger, or slog.Default() if none is set.
func (u *Upstream) log() *slog.Logger {
	if l := u.logger.Load(); l != nil {
		return l
	}
	return slog.Default()
}

// Session returns the active session, or an error if none is open.
func (u *Upstream) Session() (*mcp.ClientSession, error) {
	s := u.session.Load()
	if s == nil {
		return nil, fmt.Errorf("upstream: no active session")
	}
	return s, nil
}

// Swap connects a new session over transport and atomically makes it
// the active session, closing the previous one in the background. On
// failure the active session is left untouched and the error is
// returned, so callers may retry or keep serving on the old session.
//
// transport is consumed by a single connect attempt; pass a fresh
// transport on each call.
func (u *Upstream) Swap(ctx context.Context, transport mcp.Transport) error {
	u.swapMu.Lock()
	defer u.swapMu.Unlock()
	client := mcp.NewClient(&mcp.Implementation{Name: "mcpswap", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	prev := u.session.Swap(session)
	u.closeInBackground(prev)
	u.log().Info("upstream session opened", "id", session.ID())
	return nil
}

// Close closes the active session and clears it.
func (u *Upstream) Close() {
	u.swapMu.Lock()
	defer u.swapMu.Unlock()
	if s := u.session.Swap(nil); s != nil {
		_ = s.Close()
		u.log().Info("upstream session closed", "id", s.ID())
	}
}

func (u *Upstream) closeInBackground(s *mcp.ClientSession) {
	if s == nil {
		return
	}
	go func() {
		id := s.ID()
		done := make(chan error, 1)
		go func() { done <- s.Close() }()
		select {
		case err := <-done:
			if err != nil {
				u.log().Warn("closing previous upstream session", "id", id, "err", err)
				return
			}
			u.log().Info("upstream session closed", "id", id)
		case <-time.After(10 * time.Second):
			// The close is not aborted; we just stop waiting on it.
			u.log().Warn("previous upstream session close did not return within 10s", "id", id)
		}
	}()
}

// Dispatch is an MCP receiving middleware that forwards list/call/get/read
// requests to the active upstream session. The initialize result advertises
// only the capabilities the proxy can fulfill. Stateful methods
// (subscribe/unsubscribe/setLevel) are not proxied, and unrecognized methods
// fall through to next.
//
// Register it with (*mcp.Server).AddReceivingMiddleware(u.Dispatch).
func (u *Upstream) Dispatch(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		switch r := req.(type) {
		case *mcp.ServerRequest[*mcp.InitializeParams]:
			res, err := next(ctx, method, req)
			if init, ok := res.(*mcp.InitializeResult); ok {
				if sess, serr := u.Session(); serr == nil {
					if up := sess.InitializeResult(); up != nil && up.Capabilities != nil {
						// Advertise only what we actually proxy: drop subscribe,
						// listChanged, and logging — we forward no notifications
						// and keep no per-session state across hot-swaps.
						c := up.Capabilities
						caps := &mcp.ServerCapabilities{
							Completions:  c.Completions,
							Experimental: c.Experimental,
							Extensions:   c.Extensions,
						}
						if c.Tools != nil {
							caps.Tools = &mcp.ToolCapabilities{}
						}
						if c.Prompts != nil {
							caps.Prompts = &mcp.PromptCapabilities{}
						}
						if c.Resources != nil {
							caps.Resources = &mcp.ResourceCapabilities{}
						}
						init.Capabilities = caps
						init.Instructions = up.Instructions
						init.ServerInfo = up.ServerInfo
					}
				}
			}
			return res, err
		case *mcp.ListToolsRequest:
			sess, err := u.Session()
			if err != nil {
				return nil, err
			}
			return sess.ListTools(ctx, r.Params)
		case *mcp.CallToolRequest:
			sess, err := u.Session()
			if err != nil {
				return nil, err
			}
			return sess.CallTool(ctx, &mcp.CallToolParams{
				Meta:      r.Params.Meta,
				Name:      r.Params.Name,
				Arguments: r.Params.Arguments,
			})
		case *mcp.ListPromptsRequest:
			sess, err := u.Session()
			if err != nil {
				return nil, err
			}
			return sess.ListPrompts(ctx, r.Params)
		case *mcp.GetPromptRequest:
			sess, err := u.Session()
			if err != nil {
				return nil, err
			}
			return sess.GetPrompt(ctx, r.Params)
		case *mcp.ListResourcesRequest:
			sess, err := u.Session()
			if err != nil {
				return nil, err
			}
			return sess.ListResources(ctx, r.Params)
		case *mcp.ListResourceTemplatesRequest:
			sess, err := u.Session()
			if err != nil {
				return nil, err
			}
			return sess.ListResourceTemplates(ctx, r.Params)
		case *mcp.ReadResourceRequest:
			sess, err := u.Session()
			if err != nil {
				return nil, err
			}
			return sess.ReadResource(ctx, r.Params)
		case *mcp.CompleteRequest:
			sess, err := u.Session()
			if err != nil {
				return nil, err
			}
			return sess.Complete(ctx, r.Params)
		case *mcp.SubscribeRequest, *mcp.UnsubscribeRequest, *mcp.ServerRequest[*mcp.SetLoggingLevelParams]:
			// Stateful methods we deliberately don't proxy: resource
			// subscriptions and the logging level are per-session and would be
			// lost on a hot-swap, and we forward no notifications. We mask
			// these capabilities at initialize, so a conformant client won't
			// reach here; let the SDK reject/no-op them locally.
			return next(ctx, method, req)
		default:
			// initialized/ping/cancelled/progress and anything else are served
			// by the SDK; surface them for debugging.
			slog.Debug("unhandled method", "method", method)
			return next(ctx, method, req)
		}
	}
}
