package deviceauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// DiscoveryConfig is returned by the server's /api/auth/config endpoint
type DiscoveryConfig struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	ClientID                    string `json:"client_id"`
}

// FetchConfig fetches the discovery config from the server
func FetchConfig(serverURL string) (*DiscoveryConfig, error) {
	resp, err := http.Get(serverURL + "/api/auth/config")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var config DiscoveryConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// Issuer returns the issuer URL derived from the token endpoint
func (c *DiscoveryConfig) Issuer() (string, error) {
	u, err := url.Parse(c.TokenEndpoint)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host), nil
}
