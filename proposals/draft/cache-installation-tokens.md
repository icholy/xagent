# Cache GitHub App Installation Tokens

Issue: https://github.com/icholy/xagent/issues/781

## Problem

`githubserver.Server.CreateInstallationToken` (`internal/server/githubserver/githubserver.go:91`) mints a brand-new GitHub installation token on every call:

```go
func (s *Server) CreateInstallationToken(ctx context.Context, installationID int64) (*InstallationToken, error) {
    transport := ghinstallation.NewFromAppsTransport(s.app, installationID)
    token, err := transport.Token(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to create GitHub installation token: %w", err)
    }
    expiresAt, _, err := transport.Expiry()
    ...
    return &InstallationToken{Token: token, ExpiresAt: expiresAt}, nil
}
```

### Root cause (confirmed against the code)

`ghinstallation.Transport` caches the installation token in its `token` field and only re-fetches it when it is `nil` or near expiry. From `ghinstallation/v2@v2.18.0/transport.go`:

```go
// NewFromAppsTransport returns a Transport using an existing *AppsTransport.
func NewFromAppsTransport(atr *AppsTransport, installationID int64) *Transport {
    return &Transport{
        ...
        mu: &sync.Mutex{},   // token field left nil
    }
}

func (t *Transport) Token(ctx context.Context) (string, error) {
    t.mu.Lock()
    defer t.mu.Unlock()
    if t.token.isExpired() {        // nil token counts as expired
        if err := t.refreshToken(ctx); err != nil {   // POST .../access_tokens
            return "", ...
        }
    }
    return t.token.Token, nil
}

func (at *accessToken) isExpired() bool {
    return at == nil || at.getRefreshTime().Before(time.Now())
}
```

Each freshly-constructed transport has `token == nil`, so the first (and only) `Token()` call on it always takes the `refreshToken` branch — a `POST https://api.github.com/app/installations/{id}/access_tokens` round-trip. Because `CreateInstallationToken` constructs a transport, calls `Token()` once, and throws the transport away, the cache never survives a call. **Every** `CreateInstallationToken` call mints a fresh ~1h token over the network.

The token is valid for ~1h and `ghinstallation` refreshes it a minute before expiry (`getRefreshTime()` = `ExpiresAt - 1m`). So a single retained transport per installation would serve hundreds of calls per hour from its in-memory cache, issuing at most one network mint per ~59 minutes per installation.

This wastes latency on every caller and burns the installation's GitHub API rate limit (each mint is a request). The upcoming comment-reactions feature amplifies it: one mint per matched comment, fanned out across concurrent goroutines.

## Design

The fix is to retain the per-installation `*ghinstallation.Transport` so its internal token cache is reused across calls. Rather than bake that into `githubserver.Server`, we put it in a **self-contained, reusable type in `internal/x/githubx`** — `githubx.TokenCache`. This is pure GitHub-auth machinery, and `githubx` already owns the adjacent helpers (`ParsePrivateKey`, `ParseWebHook`). `githubserver` then holds a `*githubx.TokenCache` and delegates to it.

### `githubx.TokenCache`

A leaf helper. It imports only `ghinstallation`, `go-github`, and the LRU library — **never** `githubserver` or `store`. The dependency direction is `githubserver → githubx`, never the reverse.

```go
package githubx

import (
    "context"
    "net/http"
    "sync"
    "time"

    "github.com/bradleyfalzon/ghinstallation/v2"
    "github.com/google/go-github/v68/github"
    lru "github.com/hashicorp/golang-lru/v2"
)

// DefaultTokenCacheSize is the default number of per-installation transports
// retained by a TokenCache.
const DefaultTokenCacheSize = 256

// TokenCache issues GitHub App installation tokens, caching one
// auto-refreshing *ghinstallation.Transport per installation ID so repeated
// calls reuse the cached token instead of minting a fresh one on every call.
//
// It is safe for concurrent use.
type TokenCache struct {
    app   *ghinstallation.AppsTransport
    mu    sync.Mutex                                   // guards get-or-create on cache
    cache *lru.Cache[int64, *ghinstallation.Transport] // bounded, keyed by installation ID
}

// NewTokenCache returns a TokenCache backed by the given AppsTransport (which
// authenticates as the GitHub App). maxSize bounds the number of retained
// per-installation transports; if maxSize <= 0, DefaultTokenCacheSize is used.
func NewTokenCache(app *ghinstallation.AppsTransport, maxSize int) (*TokenCache, error) {
    if maxSize <= 0 {
        maxSize = DefaultTokenCacheSize
    }
    cache, err := lru.New[int64, *ghinstallation.Transport](maxSize)
    if err != nil {
        return nil, err
    }
    return &TokenCache{app: app, cache: cache}, nil
}

// transport returns the cached transport for an installation, creating one on
// first use. ghinstallation.Transport caches and auto-refreshes the token, so
// reusing the instance is what lets repeated calls skip the network mint.
//
// The lock is held only around the LRU lookup/insert — never across Token()'s
// network refresh. The transport's own mutex serializes refresh for a single
// installation.
func (c *TokenCache) transport(installationID int64) *ghinstallation.Transport {
    c.mu.Lock()
    defer c.mu.Unlock()
    if t, ok := c.cache.Get(installationID); ok {
        return t
    }
    t := ghinstallation.NewFromAppsTransport(c.app, installationID)
    c.cache.Add(installationID, t)
    return t
}

// Token returns a valid installation access token and its expiry, minting one
// over the network only when the cached token is absent or near expiry.
func (c *TokenCache) Token(ctx context.Context, installationID int64) (token string, expiresAt time.Time, err error) {
    t := c.transport(installationID)
    token, err = t.Token(ctx)
    if err != nil {
        return "", time.Time{}, err
    }
    expiresAt, _, err = t.Expiry()
    if err != nil {
        return "", time.Time{}, err
    }
    return token, expiresAt, nil
}

// Client returns a *github.Client authenticated as the installation, backed by
// the cached auto-refreshing transport. The client rotates its own token with
// no caller involvement.
func (c *TokenCache) Client(installationID int64) *github.Client {
    return github.NewClient(&http.Client{Transport: c.transport(installationID)})
}
```

Notes:

- The `Client` accessor folds the previously-considered option-(b) `Server.InstallationClient` *into the generic type* rather than onto the server. Because a `TokenCache` naturally owns the transport, exposing both a raw-token accessor (`Token`, for the in-container consumers) and a client accessor (`Client`, for any future server-side API caller) is free and keeps a single source of truth — both share the same cached transport, so there's no second cache to reason about.
- `github.NewClient` is from `go-github/v68`, the version `githubx` already uses (`webhook.go`). `ghinstallation.Transport` is a plain `http.RoundTripper`, so the go-github major version it wraps internally is irrelevant.

### `githubserver.Server` delegates

`githubserver` already parses the private key (`githubx.ParsePrivateKey`) and builds the `*ghinstallation.AppsTransport`. It now hands that transport to a `TokenCache` and keeps the cache instead of the bare apps transport:

```go
type Server struct {
    log       *slog.Logger
    config    *Config
    store     *store.Store
    baseURL   string
    publisher pubsub.Publisher
    tokens    *githubx.TokenCache   // was: app *ghinstallation.AppsTransport
}

func New(opts Options) (*Server, error) {
    ...
    appsTransport := ghinstallation.NewAppsTransportFromPrivateKey(http.DefaultTransport, appID, key)
    tokens, err := githubx.NewTokenCache(appsTransport, opts.Config.TokenCacheSize)
    if err != nil {
        return nil, fmt.Errorf("failed to build token cache: %w", err)
    }
    return &Server{
        ...
        tokens: tokens,
    }, nil
}

// CreateInstallationToken creates a GitHub App installation access token.
// The public signature is unchanged; it is now backed by the githubx cache.
func (s *Server) CreateInstallationToken(ctx context.Context, installationID int64) (*InstallationToken, error) {
    token, expiresAt, err := s.tokens.Token(ctx, installationID)
    if err != nil {
        return nil, fmt.Errorf("failed to create GitHub installation token: %w", err)
    }
    return &InstallationToken{Token: token, ExpiresAt: expiresAt}, nil
}
```

The max size is plumbed through `githubserver.Config` (e.g. a `TokenCacheSize int` field, `0` → default), so it can be tuned via the existing server-config path without code changes. A sensible default (`DefaultTokenCacheSize = 256`) means most deployments never set it.

### Concurrency

The reactions feature calls into the cache from multiple goroutines concurrently. The design is safe and never double-mints for a single installation:

- **Cache access** (`transport`) runs entirely under `c.mu`. The get-or-create — LRU `Get`, and on a miss `NewFromAppsTransport` + `Add` — is atomic, so only one transport is ever created per installation ID even under a concurrent burst. (`NewFromAppsTransport` does no I/O — it just allocates a struct — so holding the lock across it is cheap.)
- **The lock is never held across `Token()`'s network refresh.** `transport()` returns and releases `c.mu`; only then does `Token()` run. Refreshes for *different* installations therefore proceed in parallel.
- **Same-installation serialization is handled by the transport's own mutex.** When N goroutines share one cached transport and the token is expired, the first refreshes; the rest block on the transport's `sync.Mutex` and then return the freshly cached token without re-fetching. The library documents `RoundTrip`/`Token` as safe for concurrent use.
- **The harmless double-create note still holds as a backstop**: even if two transports were ever created for the same ID (they aren't, given the lock), the only cost is one extra network mint and a discarded transport — a perf blip, never a correctness problem.

We hold `c.mu` rather than rely on `golang-lru`'s internal locking because `Get`-then-`Add` must be a single critical section to guarantee create-once; the library's per-call locking wouldn't make the compound operation atomic on its own.

### Expiry / refresh — no manual logic

We keep zero expiry bookkeeping. `ghinstallation` owns it: `Token()` checks `isExpired()` (with a built-in 1-minute pre-expiry refresh window) and re-fetches transparently. We never compare `ExpiresAt` against the clock, never proactively refresh, never invalidate on time. The `expiresAt` we return is purely informational (surfaced through `CreateGitHubToken`'s `expires_at` response field).

## Bounded LRU (lifecycle / eviction)

The cache is a **bounded LRU** keyed by installation ID, with a configurable max size (`DefaultTokenCacheSize = 256`). When the cache is full, inserting a new installation evicts the least-recently-used one.

We deliberately do **not** rely on "installations are small and bounded." Even though that's true today, a bounded cache costs almost nothing and removes the unbounded-growth question entirely.

### Why eviction is safe and cheap

This is the key property that makes an LRU low-risk here: **a cache entry is pure performance state, never correctness state.** Evicting an installation's transport only discards its in-memory token. The next call for that installation just re-creates the transport and mints one fresh token — a single extra network round-trip. There is:

- **No correctness impact** — a re-minted token is exactly as valid as a cached one; eviction is a no-op for behavior.
- **No data loss** — nothing durable lives in the cache; GitHub is the source of truth and re-issues on demand.
- **Bounded worst case** — even a pathological eviction-thrash (more concurrently-hot installations than the cache size) degrades gracefully to "mint more often," which is just today's behavior for the thrashed entries.

So the downside of eviction is at most a perf cost on a cold entry, and the upside is a hard memory bound. That asymmetry is exactly why an LRU is the right call.

### Library: `github.com/hashicorp/golang-lru/v2`

Use `hashicorp/golang-lru/v2` (generic, well-tested, widely used) rather than hand-rolling. A hand-rolled LRU means writing and testing the eviction list + map bookkeeping for zero benefit over a mature library; the only argument for hand-rolling would be avoiding a dependency, and `golang-lru/v2` is small, dependency-light, and already common in the Go ecosystem. New direct dependency:

```
github.com/hashicorp/golang-lru/v2 v2.x
```

We use our own `sync.Mutex` around the compound get-or-create (see Concurrency) rather than `golang-lru`'s built-in synchronization, because create-once requires `Get`+`Add` to be one critical section.

## API shape

The two API options from the original draft resolve cleanly under the generic type:

- **(a) Transparent** — `githubserver.Server.CreateInstallationToken`'s signature is **unchanged**; it now delegates to `cache.Token(...)`. Every existing caller benefits with zero migration. **This is preserved and is the recommended public surface for the server.**
- **(b) Cached client accessor** — instead of a `Server.InstallationClient`, the accessor lives on the generic type as `TokenCache.Client(installationID) *github.Client`. It's available for any future *server-side* GitHub API caller (e.g. the reactions feature posting a reaction) without migrating the existing token-string consumers.

**Caller inventory** (from a repo search for `CreateInstallationToken`):

- `internal/server/apiserver/github.go:86` — `(*apiserver.Server).CreateGitHubToken` calls `s.github.CreateInstallationToken(ctx, org.GitHubInstallationID)` and maps the result into the `CreateGitHubTokenResponse` (`Token`, `ExpiresAt`). This is the single production caller. It fronts three agent-facing consumers over the Unix socket — `xagent tool git-credential`, `xagent tool github-mcp`, and the `get_github_token` MCP tool — all of which already depend on `ghinstallation`'s server-side caching being effective (the accepted `github-app-installation-tokens.md` proposal explicitly assumes "ghinstallation caches tokens server-side and re-fetches near expiry"). Today that assumption is silently false because the transport is discarded each call; this change makes it true.
- `internal/server/apiserver/github_test.go` — test coverage for `CreateGitHubToken`.

`s.github` is a concrete `*githubserver.Server`, so the body-only change to `CreateInstallationToken` needs no interface updates. The reactions feature (not yet in the repo) will reach tokens either through `CreateGitHubToken` (raw token) or, if it makes server-side API calls, through `TokenCache.Client` — both inherit the cache.

### Recommendation

1. Build `githubx.TokenCache` (with the bounded LRU and both `Token`/`Client` accessors) as the reusable mechanism.
2. Have `githubserver.Server` hold a `*githubx.TokenCache` and keep `CreateInstallationToken`'s signature unchanged (transparent option (a)). This fixes the reported waste for every current caller, including the soon-to-land reactions path, with no migration.
3. The `Client` accessor (former option (b)) ships *with* the generic type — it's free once `TokenCache` owns the transport — but no existing caller is migrated to it; it's there for the first server-side API caller that wants auto-rotating client auth.

## Tests

In `internal/x/githubx` (the cache is now unit-testable in isolation, with no `githubserver`/`store` dependencies):

- **Mint-count / reuse test**: stand up an `httptest.Server` that counts `POST /app/installations/{id}/access_tokens` requests and returns a token with an expiry ~1h out. Point the apps transport's `BaseURL` at it. Call `TokenCache.Token` N times for the same installation ID and assert exactly **one** mint reached the fake server, and that every call returned the same token and expiry. This is the regression test pinning the fix — against `master` it would see N mints.
- **Concurrency test** (`-race`): fire `Token` from many goroutines for the same installation ID (and a couple of distinct IDs). Assert one mint per distinct installation ID and no data race on the cache.
- **LRU-eviction test**: construct a `TokenCache` with `maxSize = 1`. Mint for installation A (1 mint), mint for installation B (evicts A, 1 mint), then mint for A again and assert it **re-mints** (mint count for A goes to 2). This proves eviction drops the cached token and that re-minting is a correctness no-op.
- **`Client` accessor test**: assert `TokenCache.Client(id)` returns a client whose requests carry an `Authorization` header sourced from the cached transport (one mint, reused across calls).

In `internal/server/apiserver`:

- **Existing `github_test.go`** for `CreateGitHubToken` continues to pass unchanged — the public behavior (returns a valid token + expiry) is identical; only the number of upstream mints changes.

## Trade-offs

- **Generic `githubx.TokenCache` vs. baking it into `githubserver.Server`**: the cache is pure GitHub-auth machinery with no server/store dependencies, so it belongs next to `ParsePrivateKey`/`ParseWebHook` as a reusable leaf helper. This keeps the dependency direction clean (`githubserver → githubx`), makes the cache unit-testable in isolation, and lets future non-server callers reuse it.
- **Both `Token` and `Client` accessors on the generic type**: folding the option-(b) client accessor into `TokenCache` (rather than the server) gives a single cached transport behind both a raw-token path and a client path — no duplicate cache, no migration of existing token-string consumers.
- **Bounded LRU vs. no eviction**: an earlier draft argued installations are small-and-bounded and skipped eviction. We don't bank on that — a bounded LRU costs a tiny dependency and removes the unbounded-growth question, and eviction is provably safe (drops only in-memory token state; next use re-mints). The cost is at most an occasional cold-entry mint.
- **`hashicorp/golang-lru/v2` vs. hand-rolled**: a mature generic library beats writing and testing eviction bookkeeping for no benefit; the only counter-argument (avoiding a dependency) doesn't outweigh it for a small, common library.
- **Own mutex vs. `golang-lru` internal locking**: we hold `c.mu` around `Get`+`Add` so create-once is atomic; the library's per-call locking can't make the compound op atomic on its own. The lock is never held across the network refresh.
- **Transparent (a) preserved**: `CreateInstallationToken`'s signature is unchanged, so the fix lands with zero caller migration; the client accessor is additive.

## Open Questions

- **Config surface for max size**: expose `TokenCacheSize` on `githubserver.Config` only, or also as a server flag/env var? Recommend a `Config` field defaulting to `DefaultTokenCacheSize`; a flag can be added later if anyone needs to tune it in production.
- **Eviction on uninstall**: the `InstallationEvent action: "deleted"` handler could proactively evict the uninstalled installation's entry. With the LRU bound this is purely cosmetic (a stale entry ages out on its own and holds only a dead token), so recommend not coupling the webhook handler to the cache unless the reactions work makes it convenient.
