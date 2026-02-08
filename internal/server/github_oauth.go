package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/model"
)

const (
	githubOAuthStateCookie = "xagent_github_state"
	githubOAuthStateTTL    = 10 * time.Minute
)

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
	redirectURL := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=read:user&state=%s",
		url.QueryEscape(s.github.ClientID),
		url.QueryEscape(s.baseURL+"/github/callback"),
		url.QueryEscape(state),
	)
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
	token, err := s.exchangeGitHubCode(code)
	if err != nil {
		http.Error(w, "failed to exchange code", http.StatusInternalServerError)
		return
	}

	// Fetch GitHub user info
	ghUser, err := fetchGitHubUser(token)
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
		GitHubUserID:   ghUser.ID,
		GitHubUsername: ghUser.Login,
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

type githubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

func (s *Server) exchangeGitHubCode(code string) (string, error) {
	data := url.Values{
		"client_id":     {s.github.ClientID},
		"client_secret": {s.github.ClientSecret},
		"code":          {code},
	}
	resp, err := http.PostForm("https://github.com/login/oauth/access_token", data)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading token response: %w", err)
	}
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}
	token := vals.Get("access_token")
	if token == "" {
		return "", fmt.Errorf("no access_token in response")
	}
	return token, nil
}

func fetchGitHubUser(token string) (*githubUser, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}
	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decoding GitHub user: %w", err)
	}
	return &user, nil
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
