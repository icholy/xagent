package jiraauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

const (
	stateCookie = "xagent_jira_state"
	stateTTL    = 10 * time.Minute
)

var atlassianEndpoint = oauth2.Endpoint{
	AuthURL:  "https://auth.atlassian.com/authorize",
	TokenURL: "https://auth.atlassian.com/oauth/token",
}

// Config configures the Atlassian OAuth handler.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Log          *slog.Logger
	OnSuccess    func(w http.ResponseWriter, r *http.Request, accountID, displayName string)
}

// Handler implements http.Handler for Atlassian OAuth2 login/callback.
// Mount it with http.StripPrefix so that "/login" and "/callback" are
// routed correctly.
type Handler struct {
	oauth     *oauth2.Config
	log       *slog.Logger
	onSuccess func(w http.ResponseWriter, r *http.Request, accountID, displayName string)
	mux       *http.ServeMux
}

func New(cfg Config) *Handler {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	h := &Handler{
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       []string{"read:me"},
			Endpoint:     atlassianEndpoint,
		},
		log:       log,
		onSuccess: cfg.OnSuccess,
		mux:       http.NewServeMux(),
	}
	h.mux.HandleFunc("/login", h.handleLogin)
	h.mux.HandleFunc("/callback", h.handleCallback)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := generateRandomState()
	if err != nil {
		h.log.Error("failed to generate state", "error", err)
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/jira/callback",
		MaxAge:   int(stateTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	// Atlassian OAuth 2.0 3LO requires audience and prompt parameters
	http.Redirect(w, r, h.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("audience", "api.atlassian.com"),
		oauth2.SetAuthURLParam("prompt", "consent"),
	), http.StatusFound)
}

func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(stateCookie)
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != cookie.Value {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code parameter", http.StatusBadRequest)
		return
	}
	token, err := h.oauth.Exchange(r.Context(), code)
	if err != nil {
		h.log.Error("failed to exchange code", "error", err)
		http.Error(w, "failed to exchange code", http.StatusInternalServerError)
		return
	}

	me, err := h.fetchMe(r, token.AccessToken)
	if err != nil {
		h.log.Error("failed to fetch Atlassian user", "error", err)
		http.Error(w, "failed to fetch Atlassian user", http.StatusInternalServerError)
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    "",
		Path:     "/jira/callback",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	if h.onSuccess != nil {
		h.onSuccess(w, r, me.AccountID, me.Name)
	} else {
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

type atlassianUser struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
}

func (h *Handler) fetchMe(r *http.Request, accessToken string) (*atlassianUser, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.atlassian.com/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var me atlassianUser
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return nil, err
	}
	return &me, nil
}

func generateRandomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
