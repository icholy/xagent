# OTel Spans for HTTP Middleware

Issue: https://github.com/icholy/xagent/issues/480

## Problem

The server has OTel instrumentation at two levels: `otelhttp.NewHandler` wraps the entire mux (one span per request), and `otelconnect.NewInterceptor` covers Connect RPC methods. But individual HTTP middleware — `CheckAuth`, `RequireAuth`, `AttachUserInfo`, CORS — are invisible in traces. There's no way to see how long each middleware step takes or whether a request was rejected at the auth layer vs. downstream.

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

### Span Attributes

For auth middleware specifically, it would be useful to record the auth type on the span:

```go
span.SetAttributes(attribute.String("auth.type", r.Header.Get("X-Auth-Type")))
```

This could be done inside `CheckAuth`/`RequireAuth` directly rather than in the generic wrapper, to keep `otelx.Middleware` simple and reusable. Whether to add attributes inside the auth middleware is a follow-up decision — the wrapper alone provides timing visibility.

### Resulting Trace Structure

A typical Connect RPC request would produce:

```
xagent (otelhttp)
  └── middleware.CheckAuth
       └── middleware.AttachUserInfo
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
