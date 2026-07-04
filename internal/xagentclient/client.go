//go:generate go tool moq -out client_moq.go . Client

package xagentclient

import (
	"cmp"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
)

// DefaultTimeout is the default timeout for RPC calls.
const DefaultTimeout = 30 * time.Second

// DefaultURL is the default xagent server URL.
const DefaultURL = "https://xagent.choly.ca"

type Client = xagentv1connect.XAgentServiceClient

// Options configures the xagent client.
type Options struct {
	// BaseURL is the server URL.
	BaseURL string
	// Token is the authentication token.
	// If empty, no authentication is performed.
	Token string
	// Timeout is the timeout for RPC calls.
	// Defaults to DefaultTimeout if zero.
	Timeout time.Duration
	// ClientID, when non-empty, is sent on every request as the X-Client-ID
	// header so the server stamps the resulting notifications with this id.
	// Pair with NotificationClientOptions.ClientID to suppress
	// self-notifications.
	ClientID string
	// Retry configures the retry-with-backoff behaviour for unary calls.
	// Zero-valued fields fall back to their defaults; set MaxRetries to a
	// negative value to disable retries entirely.
	Retry RetryInterceptor
}

// New returns a Connect client.
func New(opts Options) Client {
	transport := http.DefaultTransport
	if opts.Token != "" {
		transport = &AuthTransport{
			Transport: transport,
			Token:     opts.Token,
			ClientID:  opts.ClientID,
		}
	}
	httpClient := &http.Client{Transport: transport, Timeout: cmp.Or(opts.Timeout, DefaultTimeout)}
	return xagentv1connect.NewXAgentServiceClient(httpClient, opts.BaseURL,
		connect.WithInterceptors(opts.Retry),
	)
}
