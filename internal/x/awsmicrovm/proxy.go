package awsmicrovm

import (
	"context"
	"io"
	"net/http"
	"strings"
)

// ProxyAuthHeader is the header carrying a CreateMicrovmAuthToken token on
// requests to a MicroVM endpoint through the AWS proxy.
const ProxyAuthHeader = "X-aws-proxy-auth"

// NewProxyRequest builds a request to a MicroVM endpoint (the value returned by
// RunMicrovm/GetMicrovm) through the AWS proxy, with the auth token header set.
// The URL is https://{endpoint}{path}; if endpoint already carries a scheme it
// is used as-is. The caller sends the request with its own *http.Client so it
// controls timeouts and streaming/SSE.
//
// Transport only: this carries auth and routing and makes no assumptions about
// what path means or how the response is shaped. For server-sent events the
// caller sets Accept: text/event-stream and reads the body itself.
func NewProxyRequest(ctx context.Context, endpoint, token, method, path string, body io.Reader) (*http.Request, error) {
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	rawURL := strings.TrimRight(endpoint, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set(ProxyAuthHeader, token)
	return req, nil
}
