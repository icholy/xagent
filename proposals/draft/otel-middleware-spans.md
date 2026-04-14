# OTel Spans for HTTP Middleware

Issue: https://github.com/icholy/xagent/issues/480

## Problem

The server has OTel instrumentation at two levels: `otelhttp.NewHandler` wraps the entire mux (one span per request), and `otelconnect.NewInterceptor` covers Connect RPC methods. But there are two gaps:

1. **Middleware is invisible**: Individual HTTP middleware — `CheckAuth`, `RequireAuth`, `AttachUserInfo`, CORS — don't produce spans. There's no way to see how long each middleware step takes or whether a request was rejected at the auth layer vs. downstream.

2. **Non-RPC routes have no handler-level spans**: Routes like `/auth/token`, `/github/callback`, `/atlassian/callback`, `/mcp`, `/webhook/github`, and `/ui/` only get the outer `otelhttp` span. Unlike Connect RPC routes (which get `otelconnect` spans with method names), these plain HTTP handlers are opaque in traces — you can see the request happened but not which handler served it.

## Design

### Traced Middleware Wrapper

Add a helper in `internal/otelx/` that wraps a standard `func(http.Handler) http.Handler` middleware in a span:

```go
// Middleware wraps a standard HTTP middleware with an OTel span.
func Middleware(name string, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := otel.Tracer("xagent").Start(r.Context(), "middleware."+name)
            defer span.End()
            next.ServeHTTP(w, r.WithContext(ctx))
        }))
    }
}
```

The span is created _inside_ the middleware's handler, after the middleware has done its work (e.g., validated a token) but before it calls `next`. This means the span covers the downstream execution and the middleware's own work is captured as the gap between the parent span start and this child span start. Alternatively, the span could wrap the entire middleware including `next`:

```go
func Middleware(name string, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        traced := mw(next)
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := otel.Tracer("xagent").Start(r.Context(), "middleware."+name)
            defer span.End()
            traced.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

This second approach wraps the middleware entirely — the span includes both the middleware's own logic and the downstream handler. This is simpler to reason about: each span shows the total time spent in that middleware and everything below it. Nested spans naturally show the breakdown.

**Recommendation**: Use the second approach (wrap entirely). It produces a clean trace waterfall where each middleware span nests inside the previous one, and the innermost span is the actual handler.

### Usage in `server.go`

Replace bare middleware references in alice chains with traced versions:

```go
// Before
mux.Handle(path, alice.New(s.auth.CheckAuth(), s.auth.AttachUserInfo()).Then(handler))

// After
mux.Handle(path, alice.New(
    otelx.Middleware("CheckAuth", s.auth.CheckAuth()),
    otelx.Middleware("AttachUserInfo", s.auth.AttachUserInfo()),
).Then(handler))
```

Apply the same pattern to all alice chains in `Handler()`:

| Route | Middleware to trace |
|-------|-------------------|
| `/auth/token` | `CheckAuth` |
| Connect RPC | `CheckAuth`, `AttachUserInfo` |
| `/github/` | `RequireAuth`, `AttachUserInfo` |
| `/atlassian/` | `RequireAuth`, `AttachUserInfo` |
| `/mcp` | `RequireAuth`, `AttachUserInfo` |

The CORS handler and `TraceResponseHeader` are applied outside alice chains. These could optionally be wrapped too, but they're trivial (header setting) and unlikely to be useful in traces.

### Traced Handler Wrapper for Non-RPC Routes

Non-RPC routes only get the generic outer `otelhttp` span. Add a handler wrapper in `internal/otelx/` that names the span for the route:

```go
// Handler wraps an http.Handler with an OTel span named after the route.
func Handler(name string, h http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx, span := otel.Tracer("xagent").Start(r.Context(), name)
        defer span.End()
        h.ServeHTTP(w, r.WithContext(ctx))
    })
}

// HandlerFunc wraps an http.HandlerFunc with an OTel span.
func HandlerFunc(name string, f http.HandlerFunc) http.Handler {
    return Handler(name, f)
}
```

Apply to non-RPC route registrations in `server.go`:

```go
// Before
mux.HandleFunc(deviceauth.DiscoveryPath, s.handleDeviceConfig)
mux.Handle("/auth/token", alice.New(...).Then(s.auth.HandleToken()))
mux.Handle("/github/", alice.New(...).Then(http.StripPrefix("/github", gh)))
mux.Handle("/webhook/github", &webhook.GitHubHandler{...})
mux.Handle("/mcp", alice.New(...).Then(servermcp.New(s, s.baseURL).Handler()))

// After
mux.Handle(deviceauth.DiscoveryPath, otelx.HandlerFunc("DeviceDiscovery", s.handleDeviceConfig))
mux.Handle("/auth/token", alice.New(...).Then(otelx.Handler("AuthToken", s.auth.HandleToken())))
mux.Handle("/github/", alice.New(...).Then(otelx.Handler("GitHubOAuth", http.StripPrefix("/github", gh))))
mux.Handle("/webhook/github", otelx.Handler("GitHubWebhook", &webhook.GitHubHandler{...}))
mux.Handle("/mcp", alice.New(...).Then(otelx.Handler("MCP", servermcp.New(s, s.baseURL).Handler())))
```

All non-RPC routes that should get handler-level spans:

| Route | Span Name |
|-------|-----------|
| `/.well-known/oauth-authorization-server` | `OAuthMetadata` |
| `/.well-known/oauth-protected-resource` | `OAuthResourceMetadata` |
| `/oauth/register` | `OAuthRegister` |
| `/oauth/authorize` | `OAuthAuthorize` |
| `/oauth/token` | `OAuthToken` |
| `/auth/token` | `AuthToken` |
| `/auth/*` | `AuthFlow` |
| `/github/` | `GitHubOAuth` |
| `/github/webhook` | `GitHubWebhook` |
| `/atlassian/` | `AtlassianOAuth` |
| `/atlassian/webhook` | `AtlassianWebhook` |
| `/mcp` | `MCP` |
| `/ui/` | `WebUI` |
| Device discovery | `DeviceDiscovery` |

This gives non-RPC routes the same trace visibility that Connect RPC routes get from `otelconnect`.

### Resulting Trace Structure (Non-RPC)

A GitHub OAuth callback would produce:

```
xagent (otelhttp)
  └── middleware.RequireAuth          {http.request.method=GET, http.route="/github/"}
       └── middleware.AttachUserInfo  {http.request.method=GET, http.route="/github/"}
            └── GitHubOAuth           {http.request.method=GET, http.route="/github/"}
                 └── SQL query (otelsql)
```

Compared to the current state where only the outer `xagent` span is visible.

### HTTP Verb and Route Attributes

The handler wrapper should record the HTTP method and route pattern on each span using [OTel HTTP semantic conventions](https://opentelemetry.io/docs/specs/semconv/http/http-spans/):

```go
func Handler(name string, h http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx, span := otel.Tracer("xagent").Start(r.Context(), name)
        defer span.End()
        span.SetAttributes(
            semconv.HTTPRequestMethodKey.String(r.Method),
            semconv.HTTPRouteKey.String(r.Pattern),
        )
        h.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

Standard OTel semantic convention attributes:
- `http.request.method` — the HTTP verb (`GET`, `POST`, etc.)
- `http.route` — the matched route pattern from the mux

Go 1.22+ `http.ServeMux` makes `r.Pattern` available on each request (e.g., `GET /auth/token`, `/github/`). This is the matched mux pattern, which is exactly what `http.route` should contain.

For middleware spans, the same attributes can be set since the request is available:

```go
func Middleware(name string, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        traced := mw(next)
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := otel.Tracer("xagent").Start(r.Context(), "middleware."+name)
            defer span.End()
            span.SetAttributes(
                semconv.HTTPRequestMethodKey.String(r.Method),
                semconv.HTTPRouteKey.String(r.Pattern),
            )
            traced.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

**Note**: `r.Pattern` is populated by the `ServeMux` after routing, so it will be available in both middleware and handler spans since they execute after the mux has matched the route. The `semconv` package is `go.opentelemetry.io/otel/semconv/v1.26.0` (already an indirect dependency via `otelhttp`).

#### Enriching route patterns with HTTP methods

Currently routes are registered without HTTP method prefixes:

```go
mux.Handle("/auth/token", ...)   // r.Pattern = "/auth/token"
mux.Handle("/mcp", ...)          // r.Pattern = "/mcp"
```

Go 1.22+ supports method-prefixed patterns like `"GET /auth/token"`, which would make `r.Pattern` more descriptive. This is optional — the `http.request.method` attribute already carries the verb separately. But for routes that only accept one method, adding the method prefix makes the route pattern self-documenting:

```go
mux.Handle("GET /auth/token", ...)    // r.Pattern = "GET /auth/token"
mux.Handle("POST /webhook/github", ...)  // r.Pattern = "POST /webhook/github"
```

This is a minor routing change but improves trace readability. Routes that accept multiple methods (like Connect RPC's path which handles both GET and POST) should stay as-is.

### Other Span Attributes

For auth middleware specifically, it would be useful to record the auth type on the span:

```go
span.SetAttributes(attribute.String("auth.type", r.Header.Get("X-Auth-Type")))
```

This could be done inside `CheckAuth`/`RequireAuth` directly rather than in the generic wrapper, to keep `otelx.Middleware` simple and reusable. Whether to add attributes inside the auth middleware is a follow-up decision — the wrapper alone provides timing visibility.

### Resulting Trace Structure (RPC)

A typical Connect RPC request would produce:

```
xagent (otelhttp)
  └── middleware.CheckAuth            {http.request.method=POST, http.route="/xagent.v1.XAgentService/"}
       └── middleware.AttachUserInfo  {http.request.method=POST, http.route="/xagent.v1.XAgentService/"}
            └── xagent.v1.XAgentService/ListTasks (otelconnect)
                 └── SQL query (otelsql)
```

### No New Dependencies

This uses `go.opentelemetry.io/otel` which is already a dependency. No new packages needed.

## Trade-offs

**Generic wrapper vs. inline spans**: Each middleware could create its own span internally (e.g., `CheckAuth` calls `otel.Tracer().Start()` inside its handler). This gives more control over span attributes but scatters OTel concerns across middleware packages. The wrapper approach keeps middleware OTel-unaware and centralizes tracing in `server.go` + `otelx/`.

**Wrap-outside vs. wrap-inside**: The "wrap outside" approach (recommended above) is simpler but slightly less precise — the span includes both the middleware logic and downstream. The "wrap inside" approach captures only the middleware's own work in the gap, which is harder to read in trace UIs. For middleware that's primarily "check then call next," the difference is negligible.

**`otelhttp.NewMiddleware`**: The `otelhttp` package provides `NewMiddleware(operation)` which returns a `func(http.Handler) http.Handler`. However, this is designed for route-level instrumentation (records HTTP metrics, status codes, etc.) and is heavier than needed for middleware-level spans. A lightweight custom wrapper is more appropriate here.

## Open Questions

1. Should the wrapper record HTTP status codes? This would require wrapping `http.ResponseWriter`, adding complexity. The outermost `otelhttp.NewHandler` already records status codes on the root span.
2. Should auth middleware set span attributes (auth type, user ID) directly, or should that be a separate concern?
3. Should CORS and `TraceResponseHeader` also be wrapped, or are they too trivial to be worth tracing?
