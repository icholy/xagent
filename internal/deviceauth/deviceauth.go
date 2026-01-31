package deviceauth

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

var scopes = []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}

// Token stores the API key
type Token struct {
	APIKey string `json:"api_key"`
}

// Valid reports whether the token has a non-empty API key
func (t *Token) Valid() bool {
	return t != nil && t.APIKey != ""
}

// LoadToken reads a token from a JSON file.
// Returns a non-nil Token even if the file doesn't exist (with an empty API key).
func LoadToken(path string) (*Token, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Token{}, nil
		}
		return nil, err
	}
	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

// SaveToken writes a token to a JSON file.
func SaveToken(path string, token *Token) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// DeviceFlowOptions configures the device authorization flow.
type DeviceFlowOptions struct {
	DiscoveryURL string // Full URL to the discovery endpoint
	Issuer       string // ZITADEL issuer URL
	ClientID     string // Native app client ID
	Display      func(auth *oidc.DeviceAuthorizationResponse) error
}

// DeviceFlow initiates a device authorization flow and returns the access token.
func DeviceFlow(ctx context.Context, opts DeviceFlowOptions) (string, error) {
	if opts.Display == nil {
		return "", fmt.Errorf("DeviceFlow requires Display to be set")
	}

	// Fetch discovery config if DiscoveryURL is provided
	if opts.DiscoveryURL != "" {
		discovery, err := FetchDiscoveryConfig(opts.DiscoveryURL)
		if err != nil {
			return "", fmt.Errorf("fetch discovery config: %w", err)
		}
		if opts.ClientID == "" {
			opts.ClientID = discovery.ClientID
		}
		if opts.Issuer == "" {
			issuer, err := discovery.Issuer()
			if err != nil {
				return "", fmt.Errorf("parse issuer: %w", err)
			}
			opts.Issuer = issuer
		}
	}

	provider, err := rp.NewRelyingPartyOIDC(
		ctx,
		opts.Issuer,
		opts.ClientID,
		"",
		// dummy value, we don't actually use this.
		"http://localhost",
		scopes,
	)
	if err != nil {
		return "", fmt.Errorf("create relying party: %w", err)
	}

	deviceAuth, err := rp.DeviceAuthorization(ctx, scopes, provider, nil)
	if err != nil {
		return "", fmt.Errorf("device authorization: %w", err)
	}
	if err := opts.Display(deviceAuth); err != nil {
		return "", fmt.Errorf("display: %w", err)
	}
	interval := time.Duration(cmp.Or(deviceAuth.Interval, 5)) * time.Second
	tokens, err := rp.DeviceAccessToken(ctx, deviceAuth.DeviceCode, interval, provider)
	if err != nil {
		return "", fmt.Errorf("device access token: %w", err)
	}

	return tokens.AccessToken, nil
}

