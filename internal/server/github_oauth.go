package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/model"
	"golang.org/x/oauth2"
	oauth2github "golang.org/x/oauth2/github"
)

const (
	githubOAuthStateCookie = "xagent_github_state"
	githubOAuthStateTTL    = 10 * time.Minute
)

func (s *Server) githubOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.github.ClientID,
		ClientSecret: s.github.ClientSecret,
		RedirectURL:  s.baseURL + "/github/callback",
		Scopes:       []string{"read:user"},
		Endpoint:     oauth2github.Endpoint,
	}
}

// handleGitHubLogin initiates the GitHub OAuth2 flow.
func (s *Server) handleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	state, err := generateRandomState()
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}
	encrypted, err := encryptState(s.encryptionKey, state)
	if err != nil {
		http.Error(w, "failed to encrypt state", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     githubOAuthStateCookie,
		Value:    encrypted,
		Path:     "/github/callback",
		MaxAge:   int(githubOAuthStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	conf := s.githubOAuthConfig()
	redirectURL := conf.AuthCodeURL(state)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// handleGitHubCallback handles the GitHub OAuth2 callback.
func (s *Server) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(githubOAuthStateCookie)
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}
	expectedState, err := decryptState(s.encryptionKey, cookie.Value)
	if err != nil {
		http.Error(w, "invalid state cookie", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != expectedState {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	// Exchange code for access token
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code parameter", http.StatusBadRequest)
		return
	}
	conf := s.githubOAuthConfig()
	token, err := conf.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "failed to exchange code", http.StatusInternalServerError)
		return
	}

	// Fetch GitHub user info
	ghClient := github.NewClient(nil).WithAuthToken(token.AccessToken)
	ghUser, _, err := ghClient.Users.Get(r.Context(), "")
	if err != nil {
		http.Error(w, "failed to fetch GitHub user", http.StatusInternalServerError)
		return
	}

	// Upsert github_accounts row
	user := apiauth.User(r.Context())
	if user == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	account := &model.GitHubAccount{
		Owner:          user.ID,
		GitHubUserID:   ghUser.GetID(),
		GitHubUsername: ghUser.GetLogin(),
	}
	if err := s.store.CreateGitHubAccount(r.Context(), nil, account); err != nil {
		http.Error(w, "failed to save GitHub account", http.StatusInternalServerError)
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     githubOAuthStateCookie,
		Value:    "",
		Path:     "/github/callback",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/ui/settings", http.StatusFound)
}

func generateRandomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func encryptState(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

func decryptState(key []byte, encoded string) (string, error) {
	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
