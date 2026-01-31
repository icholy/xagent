package xagentclient

import (
	"cmp"
	"context"
	"net/http"
)

// TokenSource provides access tokens for authentication.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// AuthTransport injects Bearer tokens into requests.
type AuthTransport struct {
	Transport http.RoundTripper
	Source    TokenSource
	// AuthType is the value of the X-Auth-Type header.
	// Defaults to "key" if empty.
	AuthType string
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.Source.Token(req.Context())
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Auth-Type", cmp.Or(t.AuthType, "key"))
	return t.Transport.RoundTrip(req)
}
