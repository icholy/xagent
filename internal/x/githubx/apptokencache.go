package githubx

import (
	"context"
	"crypto/rsa"
	"net/http"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	lru "github.com/hashicorp/golang-lru/v2"
)

// DefaultAppTokenCacheSize bounds the number of per-installation transports retained.
const DefaultAppTokenCacheSize = 256

// AppTokenCacheOptions configures an AppTokenCache.
type AppTokenCacheOptions struct {
	AppID     int64             // GitHub App ID
	Key       *rsa.PrivateKey   // pre-parsed GitHub App private key
	Transport http.RoundTripper // base RoundTripper shared by all installations; nil -> http.DefaultTransport
	MaxSize   int               // max retained transports; <= 0 -> DefaultAppTokenCacheSize
}

// AppTokenCache issues GitHub App installation tokens, caching one
// auto-refreshing *ghinstallation.Transport per installation ID so repeated
// calls reuse the cached token instead of minting a fresh one every time.
// It is safe for concurrent use.
type AppTokenCache struct {
	appID         int64
	key           *rsa.PrivateKey
	baseTransport http.RoundTripper
	mu            sync.Mutex                                   // guards get-or-create on cache
	cache         *lru.Cache[int64, *ghinstallation.Transport] // bounded, keyed by installation ID
}

// NewAppTokenCache returns a cache that authenticates as the GitHub App
// described by opts. The caller passes an already-parsed private key, so
// transport construction never has to re-parse PEM and cannot fail on it.
func NewAppTokenCache(opts AppTokenCacheOptions) (*AppTokenCache, error) {
	if opts.MaxSize <= 0 {
		opts.MaxSize = DefaultAppTokenCacheSize
	}
	if opts.Transport == nil {
		opts.Transport = http.DefaultTransport
	}
	cache, err := lru.New[int64, *ghinstallation.Transport](opts.MaxSize)
	if err != nil {
		return nil, err
	}
	return &AppTokenCache{
		appID:         opts.AppID,
		key:           opts.Key,
		baseTransport: opts.Transport,
		cache:         cache,
	}, nil
}

// transport returns the cached transport for an installation, creating one on
// first use. ghinstallation.Transport caches and auto-refreshes the token, so
// reusing the instance is what lets repeated calls skip the network mint.
//
// The lock is held only around the LRU lookup/insert — never across Token()'s
// network refresh. Building a transport does no I/O (it allocates from the
// already-parsed key), so the critical section is cheap.
//
// Each installation gets a fully independent transport built atop a FRESH
// AppsTransport constructed from the parsed key. Nothing mutable is shared
// across installations, so the cross-installation refresh race is impossible by
// construction. The base RoundTripper is shared so TCP-connection reuse is
// preserved.
func (c *AppTokenCache) transport(installationID int64) *ghinstallation.Transport {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.cache.Get(installationID); ok {
		return t
	}
	t := ghinstallation.NewFromAppsTransport(
		ghinstallation.NewAppsTransportFromPrivateKey(c.baseTransport, c.appID, c.key),
		installationID,
	)
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

// HTTPClient returns an *http.Client authenticated as the installation, backed
// by the cached auto-refreshing transport. The transport injects the
// Authorization header and rotates the token automatically.
//
// An *http.Client (rather than a typed REST client) is what both the go-github
// REST client and the shurcooL/githubv4 GraphQL client consume, so callers can
// hand the result to whichever API they need:
//
//	rest, _ := github.NewClient(github.WithHTTPClient(cache.HTTPClient(id)))
//	gql := githubv4.NewClient(cache.HTTPClient(id))
func (c *AppTokenCache) HTTPClient(installationID int64) *http.Client {
	return &http.Client{Transport: c.transport(installationID)}
}
