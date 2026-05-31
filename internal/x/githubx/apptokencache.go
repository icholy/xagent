package githubx

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v88/github"
	lru "github.com/hashicorp/golang-lru/v2"
)

// DefaultAppTokenCacheSize bounds the number of per-installation transports retained.
const DefaultAppTokenCacheSize = 256

// AppTokenCacheOptions configures an AppTokenCache.
type AppTokenCacheOptions struct {
	AppID      int64             // GitHub App ID
	PrivateKey []byte            // GitHub App private key in PEM format
	Transport  http.RoundTripper // base RoundTripper shared by all installations; nil -> http.DefaultTransport
	MaxSize    int               // max retained transports; <= 0 -> DefaultAppTokenCacheSize
}

// AppTokenCache issues GitHub App installation tokens, caching one
// auto-refreshing *ghinstallation.Transport per installation ID so repeated
// calls reuse the cached token instead of minting a fresh one every time.
// It is safe for concurrent use.
type AppTokenCache struct {
	appID         int64
	privateKey    []byte
	baseTransport http.RoundTripper
	mu            sync.Mutex                                   // guards get-or-create on cache
	cache         *lru.Cache[int64, *ghinstallation.Transport] // bounded, keyed by installation ID
}

// NewAppTokenCache returns a cache that authenticates as the GitHub App
// described by opts. The private key is parsed up-front so configuration
// errors surface here rather than on the first token request.
func NewAppTokenCache(opts AppTokenCacheOptions) (*AppTokenCache, error) {
	if opts.MaxSize <= 0 {
		opts.MaxSize = DefaultAppTokenCacheSize
	}
	if opts.Transport == nil {
		opts.Transport = http.DefaultTransport
	}
	if _, err := ParsePrivateKey(opts.PrivateKey); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub App private key: %w", err)
	}
	cache, err := lru.New[int64, *ghinstallation.Transport](opts.MaxSize)
	if err != nil {
		return nil, err
	}
	return &AppTokenCache{
		appID:         opts.AppID,
		privateKey:    opts.PrivateKey,
		baseTransport: opts.Transport,
		cache:         cache,
	}, nil
}

// transport returns the cached transport for an installation, creating one on
// first use. ghinstallation.Transport caches and auto-refreshes the token, so
// reusing the instance is what lets repeated calls skip the network mint.
//
// The lock is held only around the LRU lookup/insert — never across Token()'s
// network refresh. ghinstallation.New does no I/O (it parses the key and
// allocates), so the critical section is cheap.
//
// Each installation gets a fully independent transport built via the public
// ghinstallation.New constructor, which constructs its own internal
// AppsTransport. Nothing mutable is shared across installations, so the
// cross-installation refresh race is impossible by construction. The base
// RoundTripper is shared so TCP-connection reuse is preserved.
func (c *AppTokenCache) transport(installationID int64) (*ghinstallation.Transport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.cache.Get(installationID); ok {
		return t, nil
	}
	t, err := ghinstallation.New(c.baseTransport, c.appID, installationID, c.privateKey)
	if err != nil {
		return nil, err
	}
	c.cache.Add(installationID, t)
	return t, nil
}

// Token returns a valid installation access token and its expiry, minting one
// over the network only when the cached transport's token is absent or near expiry.
func (c *AppTokenCache) Token(ctx context.Context, installationID int64) (string, time.Time, error) {
	t, err := c.transport(installationID)
	if err != nil {
		return "", time.Time{}, err
	}
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
func (c *AppTokenCache) Client(installationID int64) (*github.Client, error) {
	t, err := c.transport(installationID)
	if err != nil {
		return nil, err
	}
	return github.NewClient(github.WithHTTPClient(&http.Client{Transport: t}))
}
