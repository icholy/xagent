package atlassian

import (
	"context"
	"encoding/json"
	"net/http"
)

// Me represents the response from the Atlassian /me endpoint.
type Me struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
}

func FetchMe(ctx context.Context, accessToken string) (*Me, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.atlassian.com/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var me Me
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return nil, err
	}
	return &me, nil
}
