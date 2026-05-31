package githubx

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
)

// fakeGitHub is an httptest.Server standing in for GitHub's API. It serves the
// installation access-token mint endpoint, counting mints per installation ID
// and handing out a fresh, distinct token each time with a controllable expiry.
type fakeGitHub struct {
	*httptest.Server

	expiresIn time.Duration // remaining life of minted tokens

	mu       sync.Mutex
	mints    map[int64]int // mints per installation ID
	issued   int           // total tokens issued (for distinctness)
	lastAuth string        // Authorization header of the most recent non-mint request
}

func newFakeGitHub(t *testing.T, expiresIn time.Duration) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{expiresIn: expiresIn, mints: map[int64]int{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var id int64
		if n, err := fmt.Sscanf(r.URL.Path, "/app/installations/%d/access_tokens", &id); n == 1 && err == nil && r.Method == http.MethodPost {
			f.mu.Lock()
			f.mints[id]++
			f.issued++
			token := fmt.Sprintf("tok-%d-%d", id, f.issued)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      token,
				"expires_at": time.Now().UTC().Add(f.expiresIn).Format(time.RFC3339),
			})
			return
		}
		// Any other path is treated as an installation-authenticated API call.
		// Record the Authorization header so tests can assert auth was injected.
		f.mu.Lock()
		f.lastAuth = r.Header.Get("Authorization")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(f.Close)
	return f
}

// lastAuth is the Authorization header of the most recent non-mint request.
func (f *fakeGitHub) lastAuthHeader() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastAuth
}

func (f *fakeGitHub) mintCount(id int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mints[id]
}

// newTestCache builds an AppTokenCache whose apps transport is pointed at the
// fake GitHub server, using a freshly generated RSA key to sign the App JWT.
func newTestCache(t *testing.T, srv *fakeGitHub, opts AppTokenCacheOptions) *AppTokenCache {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	appsTransport := ghinstallation.NewAppsTransportFromPrivateKey(http.DefaultTransport, 12345, key)
	appsTransport.BaseURL = srv.URL
	cache, err := NewAppTokenCache(appsTransport, opts)
	if err != nil {
		t.Fatalf("NewAppTokenCache: %v", err)
	}
	return cache
}

// TestAppTokenCacheReuse pins the regression: repeated Token calls for one
// installation mint exactly once and return the same cached token. The old
// throwaway-transport behavior would mint N times.
func TestAppTokenCacheReuse(t *testing.T) {
	srv := newFakeGitHub(t, time.Hour)
	cache := newTestCache(t, srv, AppTokenCacheOptions{})

	const id = int64(42)
	first, _, err := cache.Token(context.Background(), id)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	for i := 0; i < 10; i++ {
		tok, exp, err := cache.Token(context.Background(), id)
		if err != nil {
			t.Fatalf("Token: %v", err)
		}
		if tok != first {
			t.Fatalf("token changed: got %q want %q", tok, first)
		}
		if exp.IsZero() {
			t.Fatalf("expected non-zero expiry")
		}
	}
	if got := srv.mintCount(id); got != 1 {
		t.Fatalf("mint count = %d, want 1", got)
	}
}

// TestAppTokenCacheConcurrent fires Token from many goroutines for the same and
// for distinct installations; create-once must hold and each distinct ID mints
// exactly once. Run with -race to catch cache data races.
func TestAppTokenCacheConcurrent(t *testing.T) {
	srv := newFakeGitHub(t, time.Hour)
	cache := newTestCache(t, srv, AppTokenCacheOptions{})

	ids := []int64{1, 2, 3}
	const perID = 50

	var wg sync.WaitGroup
	errs := make(chan error, len(ids)*perID)
	for _, id := range ids {
		for i := 0; i < perID; i++ {
			wg.Add(1)
			go func(id int64) {
				defer wg.Done()
				if _, _, err := cache.Token(context.Background(), id); err != nil {
					errs <- err
				}
			}(id)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Token: %v", err)
	}
	for _, id := range ids {
		if got := srv.mintCount(id); got != 1 {
			t.Fatalf("mint count for %d = %d, want 1", id, got)
		}
	}
}

// TestAppTokenCacheEviction proves eviction is a correctness no-op: with MaxSize
// 1, minting B evicts A's transport, so the next call for A re-mints.
func TestAppTokenCacheEviction(t *testing.T) {
	srv := newFakeGitHub(t, time.Hour)
	cache := newTestCache(t, srv, AppTokenCacheOptions{MaxSize: 1})

	const a, b = int64(1), int64(2)
	if _, _, err := cache.Token(context.Background(), a); err != nil {
		t.Fatalf("Token(a): %v", err)
	}
	if _, _, err := cache.Token(context.Background(), b); err != nil { // evicts a
		t.Fatalf("Token(b): %v", err)
	}
	if _, _, err := cache.Token(context.Background(), a); err != nil { // re-mints a
		t.Fatalf("Token(a) again: %v", err)
	}
	if got := srv.mintCount(a); got != 2 {
		t.Fatalf("mint count for a = %d, want 2 (re-mint after eviction)", got)
	}
	if got := srv.mintCount(b); got != 1 {
		t.Fatalf("mint count for b = %d, want 1", got)
	}
}

// TestAppTokenCacheClientAuth checks that Client carries the cached transport's
// auth and that Client and Token share one mint.
func TestAppTokenCacheClientAuth(t *testing.T) {
	srv := newFakeGitHub(t, time.Hour)
	cache := newTestCache(t, srv, AppTokenCacheOptions{})

	const id = int64(7)
	client := cache.Client(id)
	baseURL, err := client.BaseURL.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}
	client.BaseURL = baseURL

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		req, err := client.NewRequest(http.MethodGet, "some/endpoint", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		if _, err := client.Do(ctx, req, nil); err != nil {
			t.Fatalf("Do: %v", err)
		}
	}

	auth := srv.lastAuthHeader()
	if !strings.HasPrefix(auth, "token tok-") {
		t.Fatalf("Authorization header = %q, want a token from the cache", auth)
	}
	if got := srv.mintCount(id); got != 1 {
		t.Fatalf("mint count = %d, want 1 (shared transport)", got)
	}
}
