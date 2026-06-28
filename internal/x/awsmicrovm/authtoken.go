package awsmicrovm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// AllowedPort scopes which ports a CreateMicrovmAuthToken token may reach. The
// wire shape is a union: an element is either {"allPorts": {}} (every port) or
// a single port {"port": <n>}. Build elements with AllPorts or Port rather than
// constructing the struct directly.
//
// PREVIEW: the single-port shape ({"port": <n>}) is modelled from the documented
// all-ports example; only the all-ports element is shown in the public docs.
type AllowedPort struct {
	all  bool
	port int
}

// AllPorts returns an AllowedPort that authorizes every port.
func AllPorts() AllowedPort { return AllowedPort{all: true} }

// Port returns an AllowedPort that authorizes a single port.
func Port(n int) AllowedPort { return AllowedPort{port: n} }

// IsAllPorts reports whether the element authorizes every port.
func (p AllowedPort) IsAllPorts() bool { return p.all }

// PortNumber returns the authorized port and true, or 0 and false for an
// all-ports element.
func (p AllowedPort) PortNumber() (int, bool) {
	if p.all {
		return 0, false
	}
	return p.port, true
}

// MarshalJSON renders the union element: {"allPorts":{}} or {"port":<n>}.
func (p AllowedPort) MarshalJSON() ([]byte, error) {
	if p.all {
		return []byte(`{"allPorts":{}}`), nil
	}
	return json.Marshal(struct {
		Port int `json:"port"`
	}{Port: p.port})
}

// UnmarshalJSON parses the union element back into an AllowedPort.
func (p *AllowedPort) UnmarshalJSON(data []byte) error {
	var w struct {
		AllPorts *json.RawMessage `json:"allPorts"`
		Port     *int             `json:"port"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	switch {
	case w.AllPorts != nil:
		p.all = true
		p.port = 0
	case w.Port != nil:
		p.all = false
		p.port = *w.Port
	default:
		return fmt.Errorf("awsmicrovm: unrecognized allowedPort element: %s", data)
	}
	return nil
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
