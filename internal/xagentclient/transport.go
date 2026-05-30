package xagentclient

import (
	"net/http"
)

// AuthTransport injects Bearer tokens into requests.
type AuthTransport struct {
	Transport http.RoundTripper
	Token     string
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.Token)
	return t.Transport.RoundTrip(req)
}

// NewEventStreamHTTPClient returns an *http.Client suitable for long-lived
// SSE connections: no request timeout, with the bearer token attached via
// AuthTransport. Intended for token-only callers that need to talk to the
// /events endpoint via EventStreamClient.
func NewEventStreamHTTPClient(token string) *http.Client {
	return &http.Client{
		Transport: &AuthTransport{
			Transport: http.DefaultTransport,
			Token:     token,
		},
	}
}
