package xagentclient

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
)

type Client = xagentv1connect.XAgentServiceClient

// New returns a Connect client for the given base URL.
// Supports unix socket URLs: unix:///path/to/socket
func New(baseURL string, tokenSource TokenSource) Client {
	var transport http.RoundTripper = http.DefaultTransport
	if socketPath, ok := strings.CutPrefix(baseURL, "unix://"); ok {
		baseURL = "http://localhost"
		transport = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		}
	}
	if tokenSource != nil {
		transport = &AuthTransport{
			Transport: transport,
			Source:    tokenSource,
		}
	}
	httpClient := &http.Client{Transport: transport}
	return xagentv1connect.NewXAgentServiceClient(httpClient, baseURL)
}
