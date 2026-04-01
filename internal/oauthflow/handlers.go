package oauthflow

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/apiauth"
)

// HandleMetadata serves the OAuth 2.1 authorization server metadata.
// GET /.well-known/oauth-authorization-server
func (s *Server) HandleMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"issuer":                             s.baseURL,
		"authorization_endpoint":             s.baseURL + "/ui/oauth/authorize",
		"token_endpoint":                     s.baseURL + "/oauth/token",
		"registration_endpoint":              s.baseURL + "/oauth/register",
		"response_types_supported":           []string{"code"},
		"grant_types_supported":              []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":   []string{"S256"},
	})
}

// HandleResourceMetadata serves the OAuth 2.1 protected resource metadata.
// GET /.well-known/oauth-protected-resource
func (s *Server) HandleResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"resource":              s.baseURL,
		"authorization_servers": []string{s.baseURL},
	})
}

// HandleRegister is a stub DCR endpoint per RFC 7591.
// POST /oauth/register
func (s *Server) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"client_id":     uuid.New().String(),
		"client_name":   req.ClientName,
		"redirect_uris": req.RedirectURIs,
	})
}

// HandleAuthorize handles the authorization endpoint.
// POST /oauth/authorize
func (s *Server) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}
	token := r.FormValue("token")
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")
	codeChallenge := r.FormValue("code_challenge")
	codeChallengeMethod := r.FormValue("code_challenge_method")
	responseType := r.FormValue("response_type")

	if token == "" || clientID == "" || redirectURI == "" || codeChallenge == "" {
		http.Error(w, "missing required parameters", http.StatusBadRequest)
		return
	}
	if responseType != "code" {
		http.Error(w, "unsupported response_type", http.StatusBadRequest)
		return
	}
	if codeChallengeMethod != "S256" {
		http.Error(w, "unsupported code_challenge_method", http.StatusBadRequest)
		return
	}

	// Verify the app JWT
	appClaims, err := apiauth.VerifyAppToken(s.appKey, token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// Sign an auth code JWT
	now := time.Now()
	codeClaims := &authCodeClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   appClaims.Subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(authCodeTTL)),
		},
		Email:         appClaims.Email,
		Name:          appClaims.Name,
		OrgID:         appClaims.OrgID,
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		CodeChallenge: codeChallenge,
	}
	code, err := signAuthCode(s.appKey, codeClaims)
	if err != nil {
		http.Error(w, "failed to sign auth code", http.StatusInternalServerError)
		return
	}

	// Redirect back to the client
	redirect := redirectURI + "?code=" + code
	if state != "" {
		redirect += "&state=" + state
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

// HandleToken handles the token endpoint.
// POST /oauth/token
func (s *Server) HandleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	grantType := r.FormValue("grant_type")
	switch grantType {
	case "authorization_code":
		s.handleAuthCodeGrant(w, r)
	case "refresh_token":
		s.handleRefreshTokenGrant(w, r)
	default:
		http.Error(w, "unsupported grant_type", http.StatusBadRequest)
	}
}

func (s *Server) handleAuthCodeGrant(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	codeVerifier := r.FormValue("code_verifier")

	if code == "" || clientID == "" || redirectURI == "" || codeVerifier == "" {
		http.Error(w, "missing required parameters", http.StatusBadRequest)
		return
	}

	// Verify the auth code JWT
	codeClaims, err := verifyAuthCode(s.appKey, code)
	if err != nil {
		http.Error(w, "invalid authorization code", http.StatusBadRequest)
		return
	}

	// Verify client_id and redirect_uri match
	if codeClaims.ClientID != clientID {
		http.Error(w, "client_id mismatch", http.StatusBadRequest)
		return
	}
	if codeClaims.RedirectURI != redirectURI {
		http.Error(w, "redirect_uri mismatch", http.StatusBadRequest)
		return
	}

	// Verify PKCE: SHA256(code_verifier) must equal code_challenge
	h := sha256.Sum256([]byte(codeVerifier))
	computedChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	if computedChallenge != codeClaims.CodeChallenge {
		http.Error(w, "invalid code_verifier", http.StatusBadRequest)
		return
	}

	// Issue tokens
	s.issueTokens(w, codeClaims.Subject, codeClaims.Email, codeClaims.Name, codeClaims.OrgID)
}

func (s *Server) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	if refreshToken == "" {
		http.Error(w, "missing refresh_token", http.StatusBadRequest)
		return
	}

	// Verify the refresh token JWT
	claims, err := verifyRefreshToken(s.appKey, refreshToken)
	if err != nil {
		http.Error(w, "invalid refresh_token", http.StatusBadRequest)
		return
	}

	// Issue new tokens (rotation)
	s.issueTokens(w, claims.Subject, claims.Email, claims.Name, claims.OrgID)
}

func (s *Server) issueTokens(w http.ResponseWriter, subject, email, name string, orgID int64) {
	user := &apiauth.UserInfo{
		ID:    subject,
		Email: email,
		Name:  name,
		OrgID: orgID,
		Type:  apiauth.AuthTypeApp,
	}

	// Sign access token (app JWT, 5min TTL)
	appClaims := apiauth.NewAppClaims(user)
	accessToken, err := apiauth.SignAppToken(s.appKey, appClaims)
	if err != nil {
		http.Error(w, "failed to sign access token", http.StatusInternalServerError)
		return
	}

	// Sign refresh token (30 day TTL)
	now := time.Now()
	rtClaims := &refreshTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(refreshTokenTTL)),
		},
		Email: email,
		Name:  name,
		OrgID: orgID,
	}
	refreshToken, err := signRefreshToken(s.appKey, rtClaims)
	if err != nil {
		http.Error(w, "failed to sign refresh token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    int(apiauth.AppTokenTTL.Seconds()),
		"refresh_token": refreshToken,
	})
}
