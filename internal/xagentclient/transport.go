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
