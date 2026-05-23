package xagentclient

// TokenResponse is the response from the /auth/token endpoint.
type TokenResponse struct {
	Token     string `json:"token"`
	OrgID     int64  `json:"org_id"`
	ExpiresAt int64  `json:"expires_at"`
}
