package xagentclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// TokenResponse is the response from the /auth/token endpoint.
type TokenResponse struct {
	Token     string `json:"token"`
	OrgID     int64  `json:"org_id"`
	ExpiresAt int64  `json:"expires_at"`
}

// GetToken exchanges the current bearer token for an org-scoped app JWT.
func GetToken(baseURL, bearerToken string, orgID int64) (*TokenResponse, error) {
	url := baseURL + "/auth/token"
	if orgID != 0 {
		url += "?org_id=" + strconv.FormatInt(orgID, 10)
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("X-Auth-Type", "bearer")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: %s", resp.Status)
	}
	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	return &tokenResp, nil
}
