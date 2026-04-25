package deviceauth

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

var scopes = []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}

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
