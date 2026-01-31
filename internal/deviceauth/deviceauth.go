package deviceauth

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
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

// Options configures the Auth client
type Options struct {
	DiscoveryURL string // Full URL to the discovery endpoint (e.g., http://localhost:6464/device/config)
	Issuer       string // ZITADEL issuer URL (e.g., https://instance.zitadel.cloud)
	ClientID     string // Native app client ID
	ServerURL    string // Base URL of the xagent server (used to create API key)
	TokenFile    string // Path to token storage file
	KeyName      string // Name for the API key (e.g., "runner-<hostname>")
	Display      func(auth *oidc.DeviceAuthorizationResponse) error
}

// Auth handles device authorization flow and API key management
type Auth struct {
	config   Options
	provider rp.RelyingParty
	token    *Token
	mu       sync.RWMutex
	once     sync.Once
	err      error
}

// New creates a new Auth client. The provider is initialized lazily on first use.
func New(config Options) (*Auth, error) {
	if config.TokenFile == "" {
		return nil, fmt.Errorf("deviceauth.New called with empty TokenFile")
	}
	a := &Auth{config: config}
	if err := a.load(); err != nil {
		return nil, fmt.Errorf("load token file: %w", err)
	}
	return a, nil
}

// init initializes the OIDC provider. It is called lazily via once.Do().
func (a *Auth) init(ctx context.Context) error {
	a.once.Do(func() {
		// Fetch discovery config if DiscoveryURL is provided
		if a.config.DiscoveryURL != "" {
			discovery, err := FetchDiscoveryConfig(a.config.DiscoveryURL)
			if err != nil {
				a.err = fmt.Errorf("fetch discovery config: %w", err)
				return
			}
			if a.config.ClientID == "" {
				a.config.ClientID = discovery.ClientID
			}
			if a.config.Issuer == "" {
				issuer, err := discovery.Issuer()
				if err != nil {
					a.err = fmt.Errorf("parse issuer: %w", err)
					return
				}
				a.config.Issuer = issuer
			}
		}
		provider, err := rp.NewRelyingPartyOIDC(
			ctx,
			a.config.Issuer,
			a.config.ClientID,
			"",
			// dummy value, we don't actually use this.
			"http://localhost",
			scopes,
		)
		if err != nil {
			a.err = fmt.Errorf("create relying party: %w", err)
			return
		}
		a.provider = provider
	})
	return a.err
}

// ErrNoToken is returned when no valid token is available
var ErrNoToken = fmt.Errorf("no valid token available, run login to authenticate")

// Token returns the API key.
// Returns ErrNoToken if no API key exists.
func (a *Auth) Token(_ context.Context) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.token.Valid() {
		return a.token.APIKey, nil
	}
	return "", ErrNoToken
}

// DeviceFlow initiates a device authorization flow and creates an API key.
func (a *Auth) DeviceFlow(ctx context.Context) error {
	if a.config.Display == nil {
		return fmt.Errorf("DeviceFlow requires Display to be set")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.init(ctx); err != nil {
		return err
	}
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

	// Use the short-lived OIDC token to create an API key
	apiKey, err := a.createAPIKey(ctx, tokens.AccessToken)
	if err != nil {
		return fmt.Errorf("create API key: %w", err)
	}

	a.token = &Token{
		APIKey: apiKey,
	}
	if err := a.save(); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	return nil
}

// createAPIKey uses a short-lived OIDC bearer token to create an API key on the server.
func (a *Auth) createAPIKey(ctx context.Context, accessToken string) (string, error) {
	client := xagentclient.New(xagentclient.Options{
		BaseURL:  a.config.ServerURL,
		Source:   staticTokenSource(accessToken),
		AuthType: "bearer",
	})
	keyName := cmp.Or(a.config.KeyName, "runner")
	resp, err := client.CreateKey(ctx, &xagentv1.CreateKeyRequest{
		Name: keyName,
	})
	if err != nil {
		return "", err
	}
	return resp.RawToken, nil
}

type staticTokenSource string

func (s staticTokenSource) Token(_ context.Context) (string, error) {
	return string(s), nil
}

// load the token from a file
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

// save the token to a file
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
