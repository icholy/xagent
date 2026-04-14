# OTel Spans Inline in Middleware and Handlers

Issue: https://github.com/icholy/xagent/issues/480

Alternate approach to [proposals/draft/otel-middleware-spans.md](otel-middleware-spans.md) — instead of generic wrappers, each middleware and handler creates its own spans directly.

## Problem

Same as #480: individual middleware and non-RPC handlers are invisible in traces. The outer `otelhttp.NewHandler` gives one span per request; `otelconnect` covers RPC methods; everything in between is a black box.

## Design

### Approach

Each middleware and handler that wants tracing calls `otel.Tracer().Start()` directly in its own code. This couples each package to OTel but gives full control over span names, attributes, and error recording at the point where the context is richest.

### Auth Middleware (`internal/apiauth/apiauth.go`)

Add a package-level tracer and create spans inside `RequireAuth`, `CheckAuth`, and `AttachUserInfo`:

```go
var tracer = otel.Tracer("xagent/apiauth")

func (a *Auth) RequireAuth() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := tracer.Start(r.Context(), "RequireAuth",
                trace.WithAttributes(
                    semconv.HTTPRequestMethodKey.String(r.Method),
                    semconv.HTTPRouteKey.String(r.Pattern),
                    semconv.URLPathKey.String(r.URL.Path),
                ))
            defer span.End()

            switch r.Header.Get(AuthTypeHeader) {
            case AuthTypeKey:
                span.SetAttributes(attribute.String("auth.type", "key"))
                user, err := a.validateKey(r.WithContext(ctx))
                if err != nil || user == nil {
                    span.SetAttributes(attribute.Bool("auth.rejected", true))
                    http.Error(w, "invalid API key", http.StatusUnauthorized)
                    return
                }
                span.SetAttributes(
                    attribute.String("enduser.id", user.ID),
                    attribute.Int64("enduser.org_id", user.OrgID),
                )
                r = r.WithContext(apiauth.WithUser(ctx, user))
                next.ServeHTTP(w, r)
            case AuthTypeApp:
                span.SetAttributes(attribute.String("auth.type", "app"))
                // ... same pattern: set user attributes on success, auth.rejected on failure
            case AuthTypeBearer:
                span.SetAttributes(attribute.String("auth.type", "bearer"))
                // ...
            default:
                span.SetAttributes(attribute.String("auth.type", "cookie"))
                // ...
            }
        })
    }
}
```

Key differences from the wrapper approach:
- **Auth type is set at the decision point**, not inferred from a header heuristic
- **User ID and org ID are set immediately** when the user is resolved, on the same span that performed auth
- **Rejection is recorded** as `auth.rejected=true` so failed auth attempts are visible in traces
- **Error details** can be attached to the span (e.g., `span.RecordError(err)` on validation failures)

`CheckAuth` follows the same pattern but without rejecting — it just records whether auth succeeded.

`AttachUserInfo` creates a span and sets user attributes:

```go
func (a *Auth) AttachUserInfo() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := tracer.Start(r.Context(), "AttachUserInfo")
            defer span.End()
            if user := a.User(r); user != nil {
                span.SetAttributes(
                    attribute.String("enduser.id", user.ID),
                    attribute.Int64("enduser.org_id", user.OrgID),
                )
                r = r.WithContext(WithUser(ctx, user))
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

### Non-RPC Route Handlers

Each handler creates its own span with route-specific attributes. Examples:

**`HandleToken` (`internal/apiauth/apiauth.go`)**:

```go
func (a *Auth) HandleToken() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        ctx, span := tracer.Start(r.Context(), "HandleToken",
            trace.WithAttributes(
                semconv.HTTPRequestMethodKey.String(r.Method),
                semconv.HTTPRouteKey.String(r.Pattern),
            ))
        defer span.End()

        if r.Method != http.MethodGet {
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
            return
        }
        user := a.User(r)
        if user == nil {
            span.SetAttributes(attribute.Bool("auth.rejected", true))
            http.Error(w, "authentication required", http.StatusUnauthorized)
            return
        }
        span.SetAttributes(
            attribute.String("enduser.id", user.ID),
            attribute.Int64("enduser.org_id", user.OrgID),
        )
        // ... rest of handler
    }
}
```

**Webhook handlers (`internal/webhook/`)**:

```go
var tracer = otel.Tracer("xagent/webhook")

func (h *GitHubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    ctx, span := tracer.Start(r.Context(), "GitHubWebhook",
        trace.WithAttributes(
            semconv.HTTPRequestMethodKey.String(r.Method),
            semconv.HTTPRouteKey.String(r.Pattern),
            semconv.URLPathKey.String(r.URL.Path),
            attribute.String("github.event", r.Header.Get("X-GitHub-Event")),
        ))
    defer span.End()
    // ... existing handler logic using ctx
}
```

**OAuth link handlers (`internal/oauthlink/`)**:

```go
var tracer = otel.Tracer("xagent/oauthlink")

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    ctx, span := tracer.Start(r.Context(), "OAuthLink",
        trace.WithAttributes(
            semconv.HTTPRequestMethodKey.String(r.Method),
            semconv.HTTPRouteKey.String(r.Pattern),
            semconv.URLPathKey.String(r.URL.Path),
            attribute.String("oauth.provider", h.provider),
        ))
    defer span.End()
    // ... existing handler logic using ctx
}
```

**MCP handler (`internal/servermcp/`)**:

```go
var tracer = otel.Tracer("xagent/servermcp")

// In the MCP handler's ServeHTTP or initialization:
ctx, span := tracer.Start(r.Context(), "MCP",
    trace.WithAttributes(
        semconv.HTTPRequestMethodKey.String(r.Method),
        semconv.HTTPRouteKey.String(r.Pattern),
        semconv.URLPathKey.String(r.URL.Path),
    ))
defer span.End()
```

### Handling Incomplete `r.Pattern`

`r.Pattern` is set by `http.ServeMux` to the matched mux pattern, which for prefix routes is just the prefix:

| Mux registration | Actual request path | `r.Pattern` |
|-----------------|-------------------|------------|
| `/github/` | `/github/callback` | `/github/` |
| `/atlassian/` | `/atlassian/callback` | `/atlassian/` |
| `/xagent.v1.XAgentService/` | `/xagent.v1.XAgentService/ListTasks` | `/xagent.v1.XAgentService/` |
| `/auth/token` | `/auth/token` | `/auth/token` |
| `/mcp` | `/mcp` | `/mcp` |
| `/webhook/github` | `/webhook/github` | `/webhook/github` |

For prefix routes, `r.Pattern` is incomplete. To get full path visibility, set both attributes:

```go
span.SetAttributes(
    semconv.HTTPRequestMethodKey.String(r.Method),
    semconv.HTTPRouteKey.String(r.Pattern),   // mux pattern (for grouping)
    semconv.URLPathKey.String(r.URL.Path),     // full path (for detail)
)
```

- `http.route` (`r.Pattern`) — useful for grouping/aggregating traces by route pattern
- `url.path` (`r.URL.Path`) — useful for seeing the exact path hit

Both are standard OTel semantic convention attributes. `http.route` is the low-cardinality grouping key; `url.path` has full detail. For exact-match routes like `/auth/token` or `/mcp`, they're identical. For prefix routes, `url.path` shows the full path.

Alternatively, handlers that do their own sub-routing (like `oauthlink` which dispatches `/login` vs `/callback`) can set `http.route` to a more specific pattern:

```go
// Inside oauthlink handler, after determining the sub-route:
span.SetAttributes(semconv.HTTPRouteKey.String("/github/callback"))
```

### What Changes in `server.go`

Nothing. The alice chains stay as-is. `server.go` doesn't need to know about tracing — each middleware/handler is self-instrumenting:

```go
// Unchanged — tracing happens inside each middleware
mux.Handle(path, alice.New(s.auth.CheckAuth(), s.auth.AttachUserInfo()).Then(handler))
mux.Handle("/github/", alice.New(s.auth.RequireAuth(), s.auth.AttachUserInfo()).Then(...))
mux.Handle("/mcp", alice.New(s.auth.RequireAuth(), s.auth.AttachUserInfo()).Then(...))
```

### Resulting Trace Structure

An authenticated Connect RPC request:

```
xagent (otelhttp)
  └── CheckAuth                       {http.request.method=POST, http.route="/xagent.v1.XAgentService/",
                                       auth.type="cookie"}
       └── AttachUserInfo             {enduser.id="user123", enduser.org_id=1}
            └── xagent.v1.XAgentService/ListTasks (otelconnect)
                 └── SQL query (otelsql)
```

A failed auth request:

```
xagent (otelhttp)
  └── RequireAuth                     {http.request.method=GET, http.route="/mcp",
                                       auth.type="key", auth.rejected=true}
```

A GitHub webhook:

```
xagent (otelhttp)
  └── GitHubWebhook                   {http.request.method=POST, http.route="/webhook/github",
                                       url.path="/webhook/github", github.event="pull_request"}
```

A GitHub OAuth callback (prefix route — `url.path` provides the full path):

```
xagent (otelhttp)
  └── RequireAuth                     {http.request.method=GET, http.route="/github/",
                                       url.path="/github/callback", auth.type="cookie"}
       └── AttachUserInfo             {enduser.id="user123", enduser.org_id=1}
            └── OAuthLink             {http.request.method=GET, http.route="/github/",
                                       url.path="/github/callback", oauth.provider="github"}
```

### Packages That Need Changes

| Package | What to add |
|---------|------------|
| `internal/apiauth/` | Spans in `RequireAuth`, `CheckAuth`, `AttachUserInfo`, `HandleToken` |
| `internal/webhook/` | Spans in `GitHubHandler.ServeHTTP`, `AtlassianHandler.ServeHTTP` |
| `internal/oauthlink/` | Span in `Handler.ServeHTTP` |
| `internal/servermcp/` | Span in MCP handler |
| `internal/server/` | Span in `handleDeviceConfig`, `handleCORS` (optional) |
| `internal/deviceauth/` | Span in discovery handler (optional) |

### No New Dependencies

All packages already transitively depend on `go.opentelemetry.io/otel` via the module. Direct imports of `otel`, `trace`, `attribute`, and `semconv` are needed in each package.

## Trade-offs

**Pros over the wrapper approach**:
- Richer attributes: each middleware sets exactly the attributes it knows about at the point it knows them (auth type at the switch statement, user ID after validation, GitHub event type from the header)
- Error recording: `span.RecordError(err)` and `auth.rejected` can be set precisely where failures happen
- No indirection: the trace structure maps 1:1 to the code path — what you see in the trace is what the code does
- No new abstractions: no `otelx.Middleware` or `otelx.Handler` wrappers to understand

**Cons**:
- OTel imports spread across multiple packages (`apiauth`, `webhook`, `oauthlink`, `servermcp`, etc.)
- More lines changed — each middleware/handler is modified rather than wrapped from one place
- If tracing is ever removed or replaced, every package needs updating (though this is unlikely for OTel)
- Slightly more risk of inconsistent attribute naming across packages (mitigated by using `semconv` constants)

## Open Questions

1. Should `handleCORS` and `TraceResponseHeader` get inline spans? They're trivial but would complete the picture.
2. Should the tracer name follow a convention like `"xagent/{package}"` or use a single `"xagent"` tracer?
3. Should `CheckAuth` record user attributes when auth succeeds (even though `AttachUserInfo` also records them)?
