# A Reusable GitHub App Token Cache (`githubx.AppTokenCache`)

Issue: https://github.com/icholy/xagent/issues/781

## Problem

Minting a GitHub App installation token is a network round-trip to GitHub (`POST /app/installations/{id}/access_tokens`) that returns a token valid for ~1h and counts against the installation's API rate limit. Today that mint happens far more often than it should.

### Root cause (confirmed against the code)

`githubserver.Server.CreateInstallationToken` (`internal/server/githubserver/githubserver.go:91`) builds a brand-new transport on every call and immediately reads a token from it:

```go
transport := ghinstallation.NewFromAppsTransport(s.app, installationID)
token, err := transport.Token(ctx)
```

`ghinstallation.Transport` caches the token in its `token` field and only re-fetches when that field is `nil` or near expiry (`ghinstallation/v2@v2.18.0/transport.go`):

```go
func NewFromAppsTransport(atr *AppsTransport, installationID int64) *Transport {
    return &Transport{ ... mu: &sync.Mutex{} }   // token field left nil
}

func (t *Transport) Token(ctx context.Context) (string, error) {
    t.mu.Lock(); defer t.mu.Unlock()
    if t.token.isExpired() {                 // nil counts as expired
        if err := t.refreshToken(ctx); ...   // POST .../access_tokens
    }
    return t.token.Token, nil
}
```

A freshly-constructed transport has `token == nil`, so its first `Token()` call always mints over the network. Because the transport is constructed, used once, and discarded, the cache never survives a call — **every** call mints a fresh token. The upcoming comment-reactions feature amplifies this: one mint per matched comment, fanned out across concurrent goroutines.

This proposal does **not** patch that specific call site — the `CreateInstallationToken` RPC path is being reworked separately, so we don't anchor on it. Instead the root cause motivates a **reusable utility** that any GitHub-App consumer (starting with the reactions code) can use to get installation tokens without re-minting on every call: `githubx.AppTokenCache`.

### Why not just retain the `ghinstallation.Transport`?

The obvious fix — keep one `*ghinstallation.Transport` per installation and let it cache — has a sharp edge that disqualifies it for any consumer that *holds* the returned token (a git credential, an MCP upstream session, a token handed to a sub-process). The transport's refresh window is **hardcoded and not configurable**:

```go
func (at *accessToken) getRefreshTime() time.Time {
    return at.ExpiresAt.Add(-time.Minute)   // ghinstallation/v2@v2.18.0, transport.go:146
}
func (at *accessToken) isExpired() bool {
    return at == nil || at.getRefreshTime().Before(time.Now())
}
```

So a cached transport will happily return a token with as little as ~1 minute of remaining life — refreshing only once it's *within* 60 seconds of expiry. A caller that takes that token and uses it a minute later is holding an expired credential. We need a configurable safety margin, and `ghinstallation` gives us no hook to set one.

The utility therefore does **not** rely on the transport's internal cache. It uses a throwaway transport purely as a one-shot minting primitive (sign JWT → POST → read token+expiry), then **owns the caching and the TTL policy itself**, enforcing a configurable minimum remaining lifetime.

## Design

A self-contained leaf helper in `internal/x/githubx`, alongside the existing GitHub-auth machinery (`ParsePrivateKey`, `ParseWebHook`). It imports only `ghinstallation`, `go-github`, `golang-lru`, and `singleflight` — **never** `githubserver` or `store`. Dependency direction stays `githubserver → githubx`.

### `githubx.AppTokenCache`

```go
package githubx

import (
    "context"
    "net/http"
    "strconv"
    "time"

    "github.com/bradleyfalzon/ghinstallation/v2"
    "github.com/google/go-github/v68/github"
    lru "github.com/hashicorp/golang-lru/v2"
    "golang.org/x/sync/singleflight"
)

const (
    // DefaultAppTokenCacheSize bounds the number of cached installation tokens.
    DefaultAppTokenCacheSize = 256
    // DefaultAppTokenMinTTL is the minimum remaining lifetime of a returned token.
    DefaultAppTokenMinTTL = 5 * time.Minute
)

// AppTokenCacheOptions configures an AppTokenCache. Zero values fall back to the
// package defaults.
type AppTokenCacheOptions struct {
    MaxSize int           // max cached tokens; <= 0 -> DefaultAppTokenCacheSize
    MinTTL  time.Duration // min remaining lifetime of a returned token; <= 0 -> DefaultAppTokenMinTTL
}

// AppTokenCache issues GitHub App installation tokens, caching the token value
// per installation (bounded LRU) and guaranteeing every returned token has at
// least MinTTL of remaining life. It is safe for concurrent use.
type AppTokenCache struct {
    app    *ghinstallation.AppsTransport
    minTTL time.Duration
    cache  *lru.Cache[int64, cachedToken]
    group  singleflight.Group // dedupes concurrent mints, keyed by installation ID
}

type cachedToken struct {
    token     string
    expiresAt time.Time
}

// NewAppTokenCache returns a cache backed by app (which authenticates as the
// GitHub App).
func NewAppTokenCache(app *ghinstallation.AppsTransport, opts AppTokenCacheOptions) (*AppTokenCache, error) {
    if opts.MaxSize <= 0 {
        opts.MaxSize = DefaultAppTokenCacheSize
    }
    if opts.MinTTL <= 0 {
        opts.MinTTL = DefaultAppTokenMinTTL
    }
    cache, err := lru.New[int64, cachedToken](opts.MaxSize)
    if err != nil {
        return nil, err
    }
    return &AppTokenCache{app: app, minTTL: opts.MinTTL, cache: cache}, nil
}
```

### `Token` — value-cache with a MinTTL floor

```go
// Token returns a valid installation access token with at least MinTTL of
// remaining life, minting a fresh ~1h token over the network only when the
// cached value is absent or within MinTTL of expiry.
func (c *AppTokenCache) Token(ctx context.Context, installationID int64) (string, time.Time, error) {
    if tok, ok := c.fresh(installationID); ok {
        return tok.token, tok.expiresAt, nil
    }
    // Mint, deduped per installation so a concurrent burst crossing the MinTTL
    // threshold collapses to a single GitHub round-trip.
    key := strconv.FormatInt(installationID, 10)
    v, err, _ := c.group.Do(key, func() (any, error) {
        // Re-check inside the flight: a just-finished winner may have filled it.
        if tok, ok := c.fresh(installationID); ok {
            return tok, nil
        }
        tok, err := c.mint(ctx, installationID)
        if err != nil {
            return cachedToken{}, err
        }
        c.cache.Add(installationID, tok)
        return tok, nil
    })
    if err != nil {
        return "", time.Time{}, err
    }
    tok := v.(cachedToken)
    return tok.token, tok.expiresAt, nil
}

// fresh returns the cached token for an installation if it still has >= MinTTL life.
func (c *AppTokenCache) fresh(installationID int64) (cachedToken, bool) {
    tok, ok := c.cache.Get(installationID)
    if !ok || time.Until(tok.expiresAt) < c.minTTL {
        return cachedToken{}, false
    }
    return tok, true
}

// mint fetches a fresh installation token from GitHub. The throwaway transport
// is used only as a one-shot minting primitive; we keep the value, not the transport.
func (c *AppTokenCache) mint(ctx context.Context, installationID int64) (cachedToken, error) {
    t := ghinstallation.NewFromAppsTransport(c.app, installationID)
    token, err := t.Token(ctx)
    if err != nil {
        return cachedToken{}, err
    }
    expiresAt, _, err := t.Expiry()
    if err != nil {
        return cachedToken{}, err
    }
    return cachedToken{token: token, expiresAt: expiresAt}, nil
}
```

A freshly minted token has ~1h of life, comfortably above any sane `MinTTL`, so the steady state is: one mint per installation roughly every `~55m` (`1h − MinTTL`), every other call served from the value-cache.

### `Client` — same value-cache behind a `*github.Client`

`Client` returns a `*github.Client` whose transport pulls auth from the **same** `Token` path, so both accessors share one MinTTL value-cache — a single source of truth, no second cache.

```go
// Client returns a *github.Client authenticated as the installation. Each
// request fetches a token via Token(), so it benefits from the same value-cache
// and MinTTL guarantee, and auth rotates automatically as tokens expire.
func (c *AppTokenCache) Client(installationID int64) *github.Client {
    rt := &appTokenTransport{cache: c, installationID: installationID, base: http.DefaultTransport}
    return github.NewClient(&http.Client{Transport: rt})
}

type appTokenTransport struct {
    cache          *AppTokenCache
    installationID int64
    base           http.RoundTripper
}

func (t *appTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    token, _, err := t.cache.Token(req.Context(), t.installationID)
    if err != nil {
        return nil, err
    }
    req = req.Clone(req.Context()) // per RoundTripper contract: don't mutate the input
    req.Header.Set("Authorization", "token "+token)
    return t.base.RoundTrip(req)
}
```

(`go-github/v68` is the version `githubx` already uses in `webhook.go`; `ghinstallation` internally wraps a different go-github major, which is irrelevant since the transport is a plain `http.RoundTripper`.)

### Concurrency

The reactions feature calls the cache from many goroutines concurrently. The design never double-mints for a single installation and never holds a lock across the network mint:

- **Read path is lock-light.** `golang-lru/v2` is internally synchronized, so `fresh()`'s `Get` is safe on its own. The hot path (cached token with `>= MinTTL` life) returns without any mint and without a coarse lock.
- **Mint is deduped by `singleflight`, keyed by installation ID.** When a burst of goroutines for the same installation all find the cached token stale (or absent), exactly one executes the mint; the rest block on `group.Do` and receive that same result. A double-mint for one installation cannot happen. Different installations have different keys and mint in parallel.
- **No lock is held across the mint.** `singleflight` serializes only the duplicate-suppressed call; it is not a mutex we hold over unrelated work. The `lru.Cache` `Add` happens inside the flight but is a fast in-memory op.
- **Harmless-extra-mint backstop.** Even in the worst interleaving (e.g. an entry evicted between the `fresh` re-check and `Add`), the only cost is one extra network mint and a discarded token value — a perf blip, never a correctness problem.

One caveat worth noting: `singleflight.Do` runs the work under the **first** caller's `ctx`; if that caller cancels mid-flight, the shared mint is canceled for all waiters. For our usage (short mint calls, callers with comparable deadlines) this is acceptable. If per-caller cancellation isolation is ever needed, switch to `group.DoChan` + a `select` on each caller's `ctx.Done()`; called out in Open Questions.

### Bounded LRU and eviction safety

The value-cache is a **bounded LRU** keyed by installation ID, default size `DefaultAppTokenCacheSize = 256`, configurable via `AppTokenCacheOptions.MaxSize`. When full, inserting a new installation evicts the least-recently-used entry. We do **not** rely on "installations are few and bounded" — the bound makes that assumption unnecessary.

The key property: **a cache entry is pure performance state, never correctness state.** Evicting an installation's entry only discards a cached token value. The next call for that installation simply re-mints a fresh token — one extra network round-trip. There is:

- **no correctness impact** — a re-minted token is exactly as valid as a cached one; eviction is a behavioral no-op;
- **no data loss** — nothing durable lives in the cache; GitHub is the source of truth and re-issues on demand;
- **a bounded worst case** — even pathological eviction-thrash (more concurrently-hot installations than the cache size) degrades gracefully to "mint more often," i.e. today's behavior for the thrashed entries.

That asymmetry — hard memory bound up top, at most a cold-entry re-mint as the downside — is exactly why an LRU is low-risk here.

**Library: `github.com/hashicorp/golang-lru/v2`** (new direct dependency) rather than a hand-rolled LRU. It's generic, well-tested, dependency-light, and already common in the Go ecosystem; hand-rolling the eviction list + map bookkeeping would be code to write and test for no benefit. The only argument for hand-rolling is avoiding a dependency, which doesn't outweigh a small mature library.

### New dependencies

- `github.com/hashicorp/golang-lru/v2` — the bounded token-value cache.
- `golang.org/x/sync` (`singleflight`) — per-installation mint dedup. Already present in `go.mod` (`v0.20.0`); this just adds a direct import of the `singleflight` subpackage.

### Wiring (light)

`githubserver` already parses the private key (`githubx.ParsePrivateKey`) and builds the `*ghinstallation.AppsTransport`. It constructs an `AppTokenCache` from that transport and holds it, so the GitHub-App consumers that live in `githubserver` (the upcoming reactions code) call `cache.Token(...)` / `cache.Client(...)` directly:

```go
appsTransport := ghinstallation.NewAppsTransportFromPrivateKey(http.DefaultTransport, appID, key)
tokens, err := githubx.NewAppTokenCache(appsTransport, githubx.AppTokenCacheOptions{
    MaxSize: opts.Config.AppTokenCacheSize, // 0 -> default
})
// store `tokens *githubx.AppTokenCache` on the server for consumers to use
```

The max size can be surfaced through a `githubserver.Config` field (e.g. `AppTokenCacheSize int`, `0` → default) if tuning is ever wanted. This wiring is intentionally minimal: the proposal's deliverable is the utility, not a rework of any existing call site. We do **not** touch `CreateInstallationToken` or any other current caller.

## Tests

All in `internal/x/githubx`, unit-testable in isolation via an `httptest.Server` that counts `POST /app/installations/{id}/access_tokens` requests and returns a token with a controllable `expires_at`. The apps transport's `BaseURL` is pointed at the fake server.

- **Mint-count / reuse**: call `Token` N times for one installation (fresh-enough token); assert exactly **one** mint reached the server and every call returned the same token. (Against today's throwaway-transport behavior this would be N mints — the regression pin.)
- **Concurrency (`-race`)**: fire `Token` from many goroutines for the same installation (and a couple of distinct IDs); assert one mint per distinct installation ID (singleflight dedup) and no data race.
- **LRU eviction**: build with `MaxSize: 1`. Mint A (1 mint), mint B (evicts A, 1 mint), mint A again → assert A **re-mints** (A's count goes to 2). Proves eviction drops the value and re-minting is a correctness no-op.
- **MinTTL floor**: configure a `MinTTL` and have the fake server return a token whose `expires_at` is *within* `MinTTL` of now. Assert the first call mints and the *second* call mints again (the cached token is too close to expiry to reuse) — i.e. the cache never hands out a token with less than `MinTTL` of life. A companion case with `expires_at` comfortably beyond `MinTTL` asserts reuse (one mint).
- **`Client` accessor carries auth from the cache**: point `Client(id)` at a fake API endpoint; assert the outbound request carries `Authorization: token <token>` sourced from the cache, and that repeated requests reuse one mint (shared value-cache).

## Trade-offs

- **Own value-cache + MinTTL vs. retaining a `ghinstallation.Transport`**: retaining the transport would reuse its internal cache, but its refresh window is hardcoded to `ExpiresAt − 1m` and not configurable, so it can return a near-dead token to a caller that holds it. Owning the value and enforcing a configurable `MinTTL` gives every consumer a usable safety margin. The cost is that we manage the cache ourselves (LRU + singleflight) instead of leaning on the library.
- **Generic `githubx.AppTokenCache` vs. baking caching into a server method**: the cache is pure GitHub-auth machinery with no server/store dependencies, so it belongs next to `ParsePrivateKey`/`ParseWebHook` as a reusable leaf helper — unit-testable in isolation and usable by any future App consumer.
- **`Token` and `Client` on one type**: both accessors share a single MinTTL value-cache (Client's transport calls Token), so there's no duplicate cache and no migration of token-string consumers.
- **`singleflight` vs. a per-installation mutex map**: singleflight expresses "collapse concurrent duplicates into one call" directly and returns the shared result, which is exactly the mint-dedup semantics we want, without us maintaining a keyed mutex map.
- **Bounded LRU vs. unbounded map**: a tiny dependency buys a hard memory bound and removes the unbounded-growth question; eviction is provably safe (drops only a cached value; next use re-mints).
- **`hashicorp/golang-lru/v2` vs. hand-rolled**: a mature generic library beats writing and testing eviction bookkeeping for no benefit.

## Open Questions

- **`MinTTL` default**: `5m` is a conservative floor for "hold the token and use it shortly." Consumers that hold a token for a long, fixed operation could pass a larger `MinTTL`. Is per-call `MinTTL` override (vs. per-cache) worth it, or is per-cache sufficient? Recommend per-cache for now.
- **`singleflight` context sharing**: `Do` runs under the first caller's `ctx`; if cancellation isolation between concurrent callers matters, move to `DoChan` + per-caller `select`. Recommend the simple `Do` until a real need appears.
- **Config surface for `MaxSize`/`MinTTL`**: expose on `githubserver.Config` only, or also as server flags/env? Recommend `Config` fields defaulting to the package constants; flags can follow if production tuning is needed.
