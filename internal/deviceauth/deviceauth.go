package deviceauth

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

var scopes = []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail, oidc.ScopeOfflineAccess}

// Token stores the access and refresh tokens
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// Valid reports whether the access token is valid
func (t *Token) Valid() bool {
	return t != nil && t.AccessToken != "" && t.Expiry.After(time.Now().Add(time.Minute))
}

// Options configures the Auth client
type Options struct {
	DiscoveryURL string // Full URL to the discovery endpoint (e.g., http://localhost:6464/device/config)
	Issuer       string // ZITADEL issuer URL (e.g., https://instance.zitadel.cloud)
	ClientID     string // Native app client ID
	TokenFile    string // Path to token storage file
	Display      func(auth *oidc.DeviceAuthorizationResponse) error
}

// Auth handles device authorization flow and token management
type Auth struct {
	config   Options
	provider rp.RelyingParty
	token    *Token
	mu       sync.RWMutex
}

// New creates a new Auth client
func New(ctx context.Context, config Options) (*Auth, error) {
	if config.TokenFile == "" {
		return nil, fmt.Errorf("deviceauth.New called with empty TokenFile")
	}
	// Fetch discovery config if DiscoveryURL is provided
	if config.DiscoveryURL != "" {
		discovery, err := FetchDiscoveryConfig(config.DiscoveryURL)
		if err != nil {
			return nil, fmt.Errorf("fetch discovery config: %w", err)
		}
		if config.ClientID == "" {
			config.ClientID = discovery.ClientID
		}
		if config.Issuer == "" {
			issuer, err := discovery.Issuer()
			if err != nil {
				return nil, fmt.Errorf("parse issuer: %w", err)
			}
			config.Issuer = issuer
		}
	}
	provider, err := rp.NewRelyingPartyOIDC(
		ctx,
		config.Issuer,
		config.ClientID,
		"",
		// dummy value, we don't actually use this.
		"http://localhost",
		scopes,
	)
	if err != nil {
		return nil, fmt.Errorf("create relying party: %w", err)
	}
	a := &Auth{
		config:   config,
		provider: provider,
	}
	if err := a.load(); err != nil {
		return nil, fmt.Errorf("load token file: %w", err)
	}
	return a, nil
}

// ErrNoToken is returned when no valid token is available
var ErrNoToken = fmt.Errorf("no valid token available, run DeviceFlow() to authenticate")

// Token returns a valid access token, refreshing if needed.
// Returns ErrNoToken if no token exists or refresh fails.
func (a *Auth) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Token is still valid
	if a.token.Valid() {
		return a.token.AccessToken, nil
	}
	// Try to refresh
	if a.token != nil && a.token.RefreshToken != "" {
		if err := a.refresh(ctx); err == nil {
			return a.token.AccessToken, nil
		}
	}
	return "", ErrNoToken
}

// DeviceFlow initiates a new device authorization flow, even if a valid token exists
func (a *Auth) DeviceFlow(ctx context.Context) error {
	if a.config.Display == nil {
		return fmt.Errorf("DeviceFlow requires Display to be set")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	deviceAuth, err := rp.DeviceAuthorization(ctx, scopes, a.provider, nil)
	if err != nil {
		return fmt.Errorf("device authorization: %w", err)
	}
	if err := a.config.Display(deviceAuth); err != nil {
		return fmt.Errorf("display: %w", err)
	}
	interval := time.Duration(cmp.Or(deviceAuth.Interval, 5)) * time.Second
	tokens, err := rp.DeviceAccessToken(ctx, deviceAuth.DeviceCode, interval, a.provider)
	if err != nil {
		return fmt.Errorf("device access token: %w", err)
	}
	a.token = &Token{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,

		// TODO: maybe use the exp from the JWT ?
		Expiry: time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second),
	}
	if err := a.save(); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	return nil
}

// refresh the tokens
func (a *Auth) refresh(ctx context.Context) error {
	tokens, err := rp.RefreshTokens[*oidc.IDTokenClaims](ctx, a.provider, a.token.RefreshToken, "", "")
	if err != nil {
		return fmt.Errorf("refresh: %w", err)
	}
	a.token.AccessToken = tokens.AccessToken
	a.token.Expiry = tokens.Expiry
	// Update refresh token if a new one was issued
	if tokens.RefreshToken != "" {
		a.token.RefreshToken = tokens.RefreshToken
	}
	if err := a.save(); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	return nil
}

// load the tokens from the a file
func (a *Auth) load() error {
	data, err := os.ReadFile(a.config.TokenFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return err
	}
	a.token = &token
	return nil
}

// save the tokens to a file
func (a *Auth) save() error {
	if a.config.TokenFile == "" {
		return nil
	}
	data, err := json.MarshalIndent(a.token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.config.TokenFile, data, 0600)
}
