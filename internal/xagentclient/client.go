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
func New(baseURL string) Client {
	httpClient := http.DefaultClient

	if strings.HasPrefix(baseURL, "unix://") {
		socketPath := strings.TrimPrefix(baseURL, "unix://")
		baseURL = "http://localhost"
		httpClient = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
		}
	}

	return xagentv1connect.NewXAgentServiceClient(httpClient, baseURL)
}
