package ghauth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"
	oauth2github "golang.org/x/oauth2/github"
)

const (
	stateCookie = "xagent_github_state"
	stateTTL    = 10 * time.Minute
)

// Handler implements http.Handler for GitHub OAuth2 login/callback.
// Mount it with http.StripPrefix so that "/login" and "/callback" are
// routed correctly.
type Handler struct {
	oauth     *oauth2.Config
	OnSuccess func(w http.ResponseWriter, r *http.Request, user *github.User)
	mux       *http.ServeMux
}

func New(clientID, clientSecret, redirectURL string) *Handler {
	h := &Handler{
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"read:user"},
			Endpoint:     oauth2github.Endpoint,
		},
		mux: http.NewServeMux(),
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
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/github/callback",
		MaxAge:   int(stateTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, h.oauth.AuthCodeURL(state), http.StatusFound)
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
		http.Error(w, "failed to exchange code", http.StatusInternalServerError)
		return
	}

	ghClient := github.NewClient(nil).WithAuthToken(token.AccessToken)
	ghUser, _, err := ghClient.Users.Get(r.Context(), "")
	if err != nil {
		http.Error(w, "failed to fetch GitHub user", http.StatusInternalServerError)
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    "",
		Path:     "/github/callback",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	if h.OnSuccess != nil {
		h.OnSuccess(w, r, ghUser)
	} else {
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func generateRandomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
