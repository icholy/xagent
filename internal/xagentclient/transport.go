package xagentclient

import (
	"cmp"
	"net/http"
)

// AuthTransport injects Bearer tokens into requests.
type AuthTransport struct {
	Transport http.RoundTripper
	Token     string
	// AuthType is the value of the X-Auth-Type header.
	// Defaults to "key" if empty.
	AuthType string
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("X-Auth-Type", cmp.Or(t.AuthType, "key"))
	return t.Transport.RoundTrip(req)
}
