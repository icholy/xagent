package oauthflow_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/oauthflow"
)

func TestAuthorizationCodeFlow(t *testing.T) {
	key, err := apiauth.CreateAppPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	auth, err := oauthflow.New(oauthflow.Options{
		AppKey:  key,
		BaseURL: "http://localhost:8080",
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/register", auth.HandleRegister)
	mux.HandleFunc("/oauth/authorize", auth.HandleAuthorize)
	mux.HandleFunc("/oauth/token", auth.HandleToken)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Step 1: Register a client
	regBody := `{"client_name":"test-client","redirect_uris":["http://localhost/callback"]}`
	resp, err := http.Post(ts.URL+"/oauth/register", "application/json", strings.NewReader(regBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", resp.StatusCode)
	}
	var regResp map[string]any
	json.NewDecoder(resp.Body).Decode(&regResp)
	clientID := regResp["client_id"].(string)

	// Step 2: Generate PKCE
	codeVerifier := "test-verifier-string-that-is-at-least-43-characters-long"
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	// Step 3: Create app JWT
	user := &apiauth.UserInfo{
		ID:    "test-user",
		Email: "test@example.com",
		Name:  "Test User",
		OrgID: 1,
		Type:  apiauth.AuthTypeApp,
	}
	claims := apiauth.NewAppClaims(user)
	appToken, err := apiauth.SignAppToken(key, claims)
	if err != nil {
		t.Fatal(err)
	}

	// Step 4: Authorize - get auth code
	redirectURI := "http://localhost/callback"
	authForm := url.Values{
		"token":                 {appToken},
		"client_id":            {clientID},
		"redirect_uri":         {redirectURI},
		"response_type":        {"code"},
		"code_challenge":       {codeChallenge},
		"code_challenge_method": {"S256"},
		"state":                {"test-state"},
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err = client.PostForm(ts.URL+"/oauth/authorize", authForm)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize: expected 302, got %d", resp.StatusCode)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := location.Query().Get("code")
	if code == "" {
		t.Fatal("authorize: no code in redirect")
	}
	if location.Query().Get("state") != "test-state" {
		t.Fatal("authorize: state mismatch")
	}

	// Step 5: Exchange code for tokens
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {codeVerifier},
	}
	resp, err = http.PostForm(ts.URL+"/oauth/token", tokenForm)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token: expected 200, got %d", resp.StatusCode)
	}
	var tokenResp map[string]any
	json.NewDecoder(resp.Body).Decode(&tokenResp)
	accessToken, ok := tokenResp["access_token"].(string)
	if !ok || accessToken == "" {
		t.Fatal("token: missing access_token")
	}
	refreshToken, ok := tokenResp["refresh_token"].(string)
	if !ok || refreshToken == "" {
		t.Fatal("token: missing refresh_token")
	}

	// Step 6: Refresh token
	refreshForm := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	resp, err = http.PostForm(ts.URL+"/oauth/token", refreshForm)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh: expected 200, got %d", resp.StatusCode)
	}
	var refreshResp map[string]any
	json.NewDecoder(resp.Body).Decode(&refreshResp)
	if _, ok := refreshResp["access_token"].(string); !ok {
		t.Fatal("refresh: missing access_token")
	}
	if _, ok := refreshResp["refresh_token"].(string); !ok {
		t.Fatal("refresh: missing refresh_token")
	}
}
