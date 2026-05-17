package oauthflow_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/oauthflow"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/teststore"
	"golang.org/x/oauth2"
	"gotest.tools/v3/assert"
)

// testSetup wires the oauthflow handlers against a real store and returns a
// test server plus the signing key used to mint app JWTs.
func testSetup(t *testing.T) (*httptest.Server, []byte) {
	t.Helper()
	key, err := apiauth.CreateAppPrivateKey()
	assert.NilError(t, err)
	st := teststore.New(t)
	auth, err := oauthflow.New(oauthflow.Options{
		AppKey:  key,
		BaseURL: "http://localhost:8080",
		Store:   st,
	})
	assert.NilError(t, err)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/register", auth.HandleRegister)
	mux.HandleFunc("/oauth/authorize", auth.HandleAuthorize)
	mux.HandleFunc("/oauth/token", auth.HandleToken)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, key
}

// register hits /oauth/register and returns the issued client_id.
func register(t *testing.T, ts *httptest.Server, clientName string, redirectURIs []string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"client_name":   clientName,
		"redirect_uris": redirectURIs,
	})
	assert.NilError(t, err)
	resp, err := http.Post(ts.URL+"/oauth/register", "application/json", strings.NewReader(string(body)))
	assert.NilError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, resp.StatusCode, http.StatusCreated)
	var regResp map[string]any
	assert.NilError(t, json.NewDecoder(resp.Body).Decode(&regResp))
	return regResp["client_id"].(string)
}

// authorize posts to /oauth/authorize with the given form values and returns
// the raw response so tests can inspect status codes.
func authorize(t *testing.T, ts *httptest.Server, key []byte, clientID, redirectURI string) *http.Response {
	t.Helper()
	user := &apiauth.UserInfo{
		ID:    "test-user",
		Email: "test@example.com",
		Name:  "Test User",
		OrgID: 1,
		Type:  apiauth.AuthTypeApp,
	}
	appToken, err := apiauth.SignAppToken(key, apiauth.NewAppClaims(user))
	assert.NilError(t, err)
	verifier := oauth2.GenerateVerifier()
	cfg := &oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:  ts.URL + "/oauth/authorize",
			TokenURL: ts.URL + "/oauth/token",
		},
		RedirectURL: redirectURI,
	}
	authURL := cfg.AuthCodeURL("test-state", oauth2.S256ChallengeOption(verifier))
	parsed, err := url.Parse(authURL)
	assert.NilError(t, err)
	form := parsed.Query()
	form.Set("token", appToken)
	resp, err := http.PostForm(ts.URL+"/oauth/authorize", form)
	assert.NilError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestAuthorizationCodeFlow(t *testing.T) {
	ts, key := testSetup(t)

	redirectURI := "http://localhost/callback"
	clientID := register(t, ts, "test-client", []string{redirectURI})

	verifier := oauth2.GenerateVerifier()
	cfg := &oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:  ts.URL + "/oauth/authorize",
			TokenURL: ts.URL + "/oauth/token",
		},
		RedirectURL: redirectURI,
	}

	user := &apiauth.UserInfo{
		ID:    "test-user",
		Email: "test@example.com",
		Name:  "Test User",
		OrgID: 1,
		Type:  apiauth.AuthTypeApp,
	}
	appToken, err := apiauth.SignAppToken(key, apiauth.NewAppClaims(user))
	assert.NilError(t, err)

	authURL := cfg.AuthCodeURL("test-state", oauth2.S256ChallengeOption(verifier))
	parsed, err := url.Parse(authURL)
	assert.NilError(t, err)
	form := parsed.Query()
	form.Set("token", appToken)
	resp, err := http.PostForm(ts.URL+"/oauth/authorize", form)
	assert.NilError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	var authResp map[string]string
	assert.NilError(t, json.NewDecoder(resp.Body).Decode(&authResp))
	redirectURL, err := url.Parse(authResp["redirect_uri"])
	assert.NilError(t, err)
	code := redirectURL.Query().Get("code")
	assert.Assert(t, code != "")
	assert.Equal(t, redirectURL.Query().Get("state"), "test-state")

	ctx := t.Context()
	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	assert.NilError(t, err)
	assert.Assert(t, token.AccessToken != "")
	assert.Assert(t, token.RefreshToken != "")

	expiredToken := &oauth2.Token{RefreshToken: token.RefreshToken}
	newToken, err := cfg.TokenSource(ctx, expiredToken).Token()
	assert.NilError(t, err)
	assert.Assert(t, newToken.AccessToken != "")
}

func TestRegisterPersistsClient(t *testing.T) {
	ts, _ := testSetup(t)
	st := teststore.New(t)

	redirectURIs := []string{"http://localhost/callback", "http://localhost/other"}
	clientID := register(t, ts, "persist-client", redirectURIs)

	pending, err := st.GetPendingIntegration(t.Context(), nil, model.PendingIntegrationTypeMCP, clientID)
	assert.NilError(t, err)
	assert.Assert(t, pending.Options.MCP != nil)
	assert.Equal(t, pending.Options.MCP.ClientName, "persist-client")
	assert.DeepEqual(t, pending.Options.MCP.RedirectURIs, redirectURIs)
}

func TestAuthorizeRejectsUnknownClientID(t *testing.T) {
	ts, key := testSetup(t)

	resp := authorize(t, ts, key, "00000000-0000-0000-0000-000000000000", "http://localhost/callback")
	assert.Equal(t, resp.StatusCode, http.StatusBadRequest)
}

func TestAuthorizeRejectsUnregisteredRedirectURI(t *testing.T) {
	ts, key := testSetup(t)

	clientID := register(t, ts, "test-client", []string{"http://localhost/callback"})

	resp := authorize(t, ts, key, clientID, "http://evil.example.com/callback")
	assert.Equal(t, resp.StatusCode, http.StatusBadRequest)
}
