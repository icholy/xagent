//go:generate go tool moq -out client_moq.go . Client

package xagentclient

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
)

// DefaultURL is the default xagent server URL.
const DefaultURL = "https://xagent.choly.ca"

type Client = xagentv1connect.XAgentServiceClient

// Options configures the xagent client.
type Options struct {
	// BaseURL is the server URL. Supports unix socket URLs: unix:///path/to/socket
	BaseURL string
	// Token is the authentication token.
	// If empty, no authentication is performed.
	Token string
	// AuthType is the value of the X-Auth-Type header.
	// Defaults to "key" if empty.
	AuthType string
}

// New returns a Connect client.
func New(opts Options) Client {
	baseURL := opts.BaseURL
	var transport http.RoundTripper = http.DefaultTransport
	if socketPath, ok := strings.CutPrefix(baseURL, "unix://"); ok {
		baseURL = "http://localhost"
		transport = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		}
	}
	if opts.Token != "" {
		transport = &AuthTransport{
			Transport: transport,
			Token:     opts.Token,
			AuthType:  opts.AuthType,
		}
	}
	httpClient := &http.Client{Transport: transport}
	return xagentv1connect.NewXAgentServiceClient(httpClient, baseURL)
}
