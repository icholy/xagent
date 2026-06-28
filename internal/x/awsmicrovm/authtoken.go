package awsmicrovm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// AllowedPort scopes which ports a CreateMicrovmAuthToken token may reach. The
// wire shape is a union: an element is either {"allPorts": {}} (every port,
// when All is true) or a single port {"port": <n>}.
//
// PREVIEW: the single-port shape ({"port": <n>}) is modelled from the documented
// all-ports example; only the all-ports element is shown in the public docs.
type AllowedPort struct {
	All  bool
	Port int
}

// MarshalJSON renders the union element: {"allPorts":{}} or {"port":<n>}.
func (p AllowedPort) MarshalJSON() ([]byte, error) {
	if p.All {
		return []byte(`{"allPorts":{}}`), nil
	}
	return json.Marshal(struct {
		Port int `json:"port"`
	}{Port: p.Port})
}

// CreateMicrovmAuthTokenInput is the input to CreateMicrovmAuthToken.
type CreateMicrovmAuthTokenInput struct {
	MicrovmID         string        // -> microvmIdentifier
	ExpirationMinutes int           // -> expirationInMinutes
	AllowedPorts      []AllowedPort // -> allowedPorts
}

// CreateMicrovmAuthTokenOutput is the result of CreateMicrovmAuthToken.
type CreateMicrovmAuthTokenOutput struct {
	// Token is the proxy auth token, pulled from the response's
	// authToken["X-aws-proxy-auth"]. Pass it to NewProxyRequest.
	Token string
}

// createMicrovmAuthTokenRequest is the modelled request body.
type createMicrovmAuthTokenRequest struct {
	MicrovmID         string        `json:"microvmIdentifier"`
	ExpirationMinutes int           `json:"expirationInMinutes,omitempty"`
	AllowedPorts      []AllowedPort `json:"allowedPorts,omitempty"`
}

// createMicrovmAuthTokenResponse is the modelled response body. The token is a
// header-name/value map; the proxy auth header key is ProxyAuthHeader.
type createMicrovmAuthTokenResponse struct {
	AuthToken map[string]string `json:"authToken"`
}

// CreateMicrovmAuthToken mints a short-lived token for authenticated requests to
// a MicroVM's endpoint through the AWS proxy. Pair it with NewProxyRequest.
//
// PREVIEW: the request path (POST /microvms/<id>/auth-tokens) and JSON field
// names are modelled from the public documentation, like the rest of this
// package.
func (c *Client) CreateMicrovmAuthToken(ctx context.Context, in *CreateMicrovmAuthTokenInput) (*CreateMicrovmAuthTokenOutput, error) {
	path := "/microvms/" + url.PathEscape(in.MicrovmID) + "/auth-tokens"
	body := createMicrovmAuthTokenRequest{
		MicrovmID:         in.MicrovmID,
		ExpirationMinutes: in.ExpirationMinutes,
		AllowedPorts:      in.AllowedPorts,
	}
	var resp createMicrovmAuthTokenResponse
	if err := c.do(ctx, "CreateMicrovmAuthToken", http.MethodPost, path, body, &resp); err != nil {
		return nil, err
	}
	return &CreateMicrovmAuthTokenOutput{Token: resp.AuthToken[ProxyAuthHeader]}, nil
}
