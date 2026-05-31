package githubx

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v68/github"
	lru "github.com/hashicorp/golang-lru/v2"
)

// DefaultAppTokenCacheSize bounds the number of per-installation transports retained.
const DefaultAppTokenCacheSize = 256

// AppTokenCacheOptions configures an AppTokenCache. Zero values fall back to the
// package defaults.
type AppTokenCacheOptions struct {
	MaxSize int // max retained transports; <= 0 -> DefaultAppTokenCacheSize

	// BaseURL overrides the GitHub REST API base URL for clients returned by
	// Client. Empty means the go-github default (https://api.github.com/). Set
	// it for GitHub Enterprise or to point clients at a test server. It does not
	// affect the token-mint endpoint, which is governed by the AppsTransport's
	// own BaseURL.
	BaseURL string
}

// AppTokenCache issues GitHub App installation tokens, caching one
// auto-refreshing *ghinstallation.Transport per installation ID so repeated
// calls reuse the cached token instead of minting a fresh one every time.
// It is safe for concurrent use.
type AppTokenCache struct {
	app     *ghinstallation.AppsTransport
	baseURL *url.URL                                     // REST API base for Client; nil -> go-github default
	mu      sync.Mutex                                   // guards get-or-create on cache
	cache   *lru.Cache[int64, *ghinstallation.Transport] // bounded, keyed by installation ID
}

// NewAppTokenCache returns a cache backed by app (which authenticates as the
// GitHub App).
func NewAppTokenCache(app *ghinstallation.AppsTransport, opts AppTokenCacheOptions) (*AppTokenCache, error) {
	if opts.MaxSize <= 0 {
		opts.MaxSize = DefaultAppTokenCacheSize
	}
	cache, err := lru.New[int64, *ghinstallation.Transport](opts.MaxSize)
	if err != nil {
		return nil, err
	}
	var baseURL *url.URL
	if opts.BaseURL != "" {
		// go-github resolves request paths against BaseURL, which must end in a
		// trailing slash for ResolveReference to keep the configured path.
		u, err := url.Parse(strings.TrimSuffix(opts.BaseURL, "/") + "/")
		if err != nil {
			return nil, fmt.Errorf("invalid base url: %w", err)
		}
		baseURL = u
	}
	return &AppTokenCache{app: app, baseURL: baseURL, cache: cache}, nil
}

// transport returns the cached transport for an installation, creating one on
// first use. ghinstallation.Transport caches and auto-refreshes the token, so
// reusing the instance is what lets repeated calls skip the network mint.
//
// The lock is held only around the LRU lookup/insert — never across Token()'s
// network refresh. NewFromAppsTransport does no I/O (it just allocates), so the
// critical section is cheap.
//
// Each installation gets its own shallow copy of the apps transport. The
// library's refreshToken writes back to appsTransport.BaseURL/.Client on every
// refresh; if every per-installation transport shared one *AppsTransport, those
// writes would race across installations refreshing concurrently. The copy
// isolates the mutated fields while still sharing the underlying RoundTripper
// and JWT signer, so TCP-connection reuse is preserved.
func (c *AppTokenCache) transport(installationID int64) *ghinstallation.Transport {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.cache.Get(installationID); ok {
		return t
	}
	app := *c.app // c.app is never mutated after construction; the copy is.
	t := ghinstallation.NewFromAppsTransport(&app, installationID)
	c.cache.Add(installationID, t)
	return t
}

// Token returns a valid installation access token and its expiry, minting one
// over the network only when the cached transport's token is absent or near expiry.
func (c *AppTokenCache) Token(ctx context.Context, installationID int64) (string, time.Time, error) {
	t := c.transport(installationID)
	token, err := t.Token(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt, _, err := t.Expiry()
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// Client returns a *github.Client authenticated as the installation, backed by
// the cached auto-refreshing transport. The transport injects the Authorization
// header and rotates the token automatically; no custom RoundTripper is needed.
func (c *AppTokenCache) Client(installationID int64) *github.Client {
	client := github.NewClient(&http.Client{Transport: c.transport(installationID)})
	if c.baseURL != nil {
		client.BaseURL = c.baseURL
	}
	return client
}
