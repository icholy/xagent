package xagentclient

import (
	"context"
	"net/http"
	"strings"
)

// TokenSource provides access tokens for authentication.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// BearerTokenSource is an optional interface that indicates the token
// should be sent with X-Auth-Type: bearer (for OIDC JWT tokens).
// API keys do not need this header.
type BearerTokenSource interface {
	IsBearer() bool
}

// AuthTransport injects Bearer tokens into requests.
type AuthTransport struct {
	Transport http.RoundTripper
	Source    TokenSource
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.Source.Token(req.Context())
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+token)
	// Only set X-Auth-Type: bearer for non-API-key tokens (OIDC JWTs).
	// API keys (xat_ prefix) are auto-detected by the server middleware.
	if isBearer(t.Source, token) {
		req.Header.Set("X-Auth-Type", "bearer")
	}
	return t.Transport.RoundTrip(req)
}

func isBearer(source TokenSource, token string) bool {
	if b, ok := source.(BearerTokenSource); ok {
		return b.IsBearer()
	}
	return !strings.HasPrefix(token, "xat_")
}
