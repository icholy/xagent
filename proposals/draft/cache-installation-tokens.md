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

### Cache the transport on the Server

`ghinstallation.NewFromAppsTransport(s.app, installationID)` is cheap to build but expensive to *use* once (the network mint). The fix is to build one transport per installation ID and keep it alive on the `Server` so its internal token cache is reused.

Add a guarded map to `githubserver.Server`:

```go
type Server struct {
    log       *slog.Logger
    config    *Config
    store     *store.Store
    baseURL   string
    publisher pubsub.Publisher
    app       *ghinstallation.AppsTransport

    mu         sync.Mutex                          // guards transports
    transports map[int64]*ghinstallation.Transport // keyed by installation ID
}
```

Initialize the map in `New`:

```go
return &Server{
    ...
    app:        ghinstallation.NewAppsTransportFromPrivateKey(http.DefaultTransport, appID, key),
    transports: make(map[int64]*ghinstallation.Transport),
}, nil
```

Rework `CreateInstallationToken` to fetch-or-create the transport, then call `Token()` on the retained instance:

```go
func (s *Server) CreateInstallationToken(ctx context.Context, installationID int64) (*InstallationToken, error) {
    transport := s.transport(installationID)
    token, err := transport.Token(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to create GitHub installation token: %w", err)
    }
    expiresAt, _, err := transport.Expiry()
    if err != nil {
        return nil, fmt.Errorf("failed to get token expiry: %w", err)
    }
    return &InstallationToken{Token: token, ExpiresAt: expiresAt}, nil
}

// transport returns the cached transport for an installation, creating one on
// first use. ghinstallation.Transport caches and auto-refreshes the token, so
// reusing the instance is what lets repeated calls skip the network mint.
func (s *Server) transport(installationID int64) *ghinstallation.Transport {
    s.mu.Lock()
    defer s.mu.Unlock()
    t, ok := s.transports[installationID]
    if !ok {
        t = ghinstallation.NewFromAppsTransport(s.app, installationID)
        s.transports[installationID] = t
    }
    return t
}
```

The lock is held only for the map lookup/insert, not across `Token()` — so the (possibly network-bound) refresh inside `Token()` runs unlocked and concurrent callers for *different* installations never serialize. `Token()` has its own per-transport mutex, so concurrent callers for the *same* installation are correctly serialized inside the library: the first to find an expired token refreshes; the rest block on `t.mu` and then return the freshly cached token without re-fetching.

### Concurrency

The reactions feature calls `CreateInstallationToken` from multiple goroutines concurrently. The design is safe:

- **Map access** is guarded by `s.mu`. Concurrent `transport()` calls can't corrupt the map or double-insert under the lock.
- **`Token()` is safe for concurrent use** — the library documents `RoundTrip` as concurrency-safe and `Token()` guards `t.token` with the transport's own `sync.Mutex`. Two goroutines sharing one transport will not double-mint: the second waits on the mutex and sees the cached token.
- **No double-create race**: because the whole lookup-or-create runs under `s.mu`, only one transport is ever created per installation ID. (Even if we used a lock-free path and two goroutines briefly created two transports for the same ID, the only cost would be one extra harmless network mint and a discarded transport — no correctness issue. We hold the lock anyway because it's trivial and removes the question.)

`sync.Map` is an alternative to the explicit mutex. We recommend the plain `map` + `sync.Mutex` here: the map is tiny, writes are rare (once per installation, ever), and the explicit lock keeps the "create exactly once" invariant obvious. `sync.Map`'s `LoadOrStore` would require constructing the transport eagerly on every call (to pass as the candidate value), or a `LoadOrStore`-of-a-`sync.Once` dance — more code for no measurable benefit at this scale.

### Expiry / refresh — no manual logic

We deliberately keep zero expiry bookkeeping on our side. `ghinstallation` owns it: `Token()` checks `isExpired()` (which builds in a 1-minute pre-expiry refresh window) and re-fetches transparently. We never compare `ExpiresAt` against the clock, never proactively refresh, never invalidate. The `ExpiresAt` we return to callers is purely informational (it's already surfaced through `CreateGitHubToken`'s `expires_at` response field).

## API shape

Two options were weighed.

### (a) Transparent — keep the signature, back it with the cache **(recommended)**

`CreateInstallationToken(ctx, installationID) (*InstallationToken, error)` is unchanged. Only the body changes (retain the transport instead of discarding it). Every existing caller benefits with zero migration.

**Callers inventory** (from a repo search for `CreateInstallationToken`):

- `internal/server/apiserver/github.go:86` — `(*apiserver.Server).CreateGitHubToken` calls `s.github.CreateInstallationToken(ctx, org.GitHubInstallationID)` and maps the result into the `CreateGitHubTokenResponse` (`Token`, `ExpiresAt`). This is the single production caller. It in turn fronts three agent-facing consumers over the Unix socket — `xagent tool git-credential`, `xagent tool github-mcp`, and the `get_github_token` MCP tool — all of which already depend on `ghinstallation`'s server-side caching being effective (the accepted `github-app-installation-tokens.md` proposal explicitly assumes "ghinstallation caches tokens server-side and re-fetches near expiry"). Today that assumption is silently false because the transport is discarded each call; this change makes it true.
- `internal/server/apiserver/github_test.go` — test coverage for `CreateGitHubToken`.

`s.github` is a concrete `*githubserver.Server`, so a body-only change needs no interface updates.

The reactions feature (not yet in the repo) will presumably reach tokens through the same `CreateGitHubToken` path or a sibling `githubserver` method; either way it inherits the cache for free.

### (b) Expose a cached client accessor

Add `Server.InstallationClient(installationID) (*github.Client, error)` that returns a `*github.Client` whose HTTP transport is the cached `ghinstallation.Transport`, and migrate callers to use it instead of minting a raw token and building a client themselves.

This is attractive *if* server-side code wants to make GitHub API calls directly with auto-rotating auth (the reactions feature might — posting a reaction is a server-side API call). A `github.Client` built on the cached transport rotates its own token with no caller involvement, which is strictly nicer than handing out a raw string that the caller must re-request near expiry.

But it doesn't replace (a): the existing `CreateGitHubToken` RPC must still return a raw token string for the in-container consumers (git credential helper, github-mcp adapter, `get_github_token`) — they need the bearer token itself, not a server-side `*github.Client`. So (b) is *additive*, not a substitute.

### Recommendation

Do **(a) now** — it's a small, self-contained body change that fixes the reported waste for every current caller including the soon-to-land reactions path, with no migration. Treat **(b) as a follow-up** to be added *only when* a concrete server-side caller wants to make GitHub API calls directly (e.g. the reactions feature posting reactions). At that point `InstallationClient` becomes a thin wrapper:

```go
// InstallationClient returns a *github.Client authenticated as the installation,
// backed by the cached auto-refreshing transport.
func (s *Server) InstallationClient(installationID int64) *github.Client {
    t := s.transport(installationID)
    return github.NewClient(&http.Client{Transport: t})
}
```

It shares the exact same cached transport, so the two accessors stay consistent and there's no second cache to reason about. Building it speculatively now, with no caller, would be dead code — defer it.

## Lifecycle / eviction

The map is keyed by installation ID and grows by one entry the first time each installation is used. The number of installations is bounded by how many orgs have installed the GitHub App — small (tens to low hundreds at most), and each entry is a lightweight struct holding one cached token. No eviction is needed:

- **Memory** is negligible and bounded by a quantity that's inherently small and slow-growing.
- **Stale entries are harmless**: an entry for an uninstalled app just holds a token that will never be refreshed again. The `InstallationEvent action: "deleted"` webhook already clears the org→installation mapping, so we'll stop *requesting* tokens for a removed installation; the orphaned transport sits idle. We could prune it on the deleted event for tidiness, but it's not required for correctness and adds a coupling between the webhook handler and the token cache that isn't worth it for a few bytes.

If the installation count were ever expected to be unbounded (it isn't — it's gated by real-world app installs), an LRU or a deleted-event prune would be warranted. We explicitly choose not to build that now and note the assumption so a future reviewer can revisit if the deployment model changes.

## Tests

- **Unit test in `internal/server/githubserver`**: stand up an `httptest.Server` that counts `POST /app/installations/{id}/access_tokens` requests and returns a token with an expiry ~1h out. Point the transport's `BaseURL` at it (override on the cached `*ghinstallation.Transport`, or inject via the apps transport). Call `CreateInstallationToken` N times for the same installation ID and assert exactly **one** mint request reached the fake server, and that every call returned the same token string and expiry. This is the regression test that pins the fix — against `master` it would see N mints.
- **Concurrency test**: fire `CreateInstallationToken` from many goroutines for the same installation ID (and a couple of distinct IDs), with `-race`. Assert one mint per distinct installation ID and no data race on the map.
- **Existing `github_test.go`** for `CreateGitHubToken` continues to pass unchanged — the public behavior (returns a valid token + expiry) is identical; only the number of upstream mints changes.

## Trade-offs

- **`map` + `sync.Mutex` vs `sync.Map`**: chosen the explicit mutex for a clearer create-once invariant at trivial scale (see Concurrency). `sync.Map` buys nothing here.
- **Holding the lock only around the map op, not across `Token()`**: keeps refreshes for different installations parallel. The library's own per-transport mutex handles same-installation serialization, so we don't need to (and shouldn't) hold `s.mu` across the network call.
- **Transparent (a) vs. client accessor (b)**: (a) ships the fix with no migration; (b) is deferred until a real server-side API caller exists, at which point it layers on top of the same cache.
- **No eviction**: justified by the bounded, small installation count; revisit only if the deployment model makes installations unbounded.

## Open Questions

- Should the `InstallationEvent action: "deleted"` handler prune the corresponding transport from the cache? Not required for correctness; a tidiness-only optimization. Recommend deferring unless the reactions work makes it convenient.
- The reactions feature may want server-side `*github.Client` access (option (b)). If it lands first, fold `InstallationClient` in alongside this change rather than after.
