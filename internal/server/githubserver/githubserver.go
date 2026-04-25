// Package githubserver handles GitHub App OAuth account linking and
// webhook event routing.
package githubserver

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/go-github/v68/github"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/auth/oauthlink"
	"github.com/icholy/xagent/internal/server/webhookserver"
	"github.com/icholy/xagent/internal/store"
	"golang.org/x/oauth2"
	oauth2github "golang.org/x/oauth2/github"
)

// Config holds GitHub App configuration.
type Config struct {
	AppID         string
	AppSlug       string
	ClientID      string
	ClientSecret  string
	WebhookSecret string
}

// Server handles GitHub OAuth and webhook routes.
type Server struct {
	log     *slog.Logger
	config  *Config
	store   *store.Store
	baseURL string
}

// Options configures a Server.
type Options struct {
	Log     *slog.Logger
	Config  *Config
	Store   *store.Store
	BaseURL string
}

// New returns a new Server.
func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		log:     log,
		config:  opts.Config,
		store:   opts.Store,
		baseURL: opts.BaseURL,
	}
}

// AppInstallURL returns the GitHub App installation URL, or empty string
// if no app slug is configured.
func (s *Server) AppInstallURL() string {
	if s.config.AppSlug == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/apps/%s/installations/new", s.config.AppSlug)
}

// OAuthHandler returns the HTTP handler for GitHub OAuth account linking.
// The caller is responsible for wrapping it with authentication middleware.
func (s *Server) OAuthHandler() http.Handler {
	return oauthlink.New(oauthlink.Config{
		Provider:     "github",
		ClientID:     s.config.ClientID,
		ClientSecret: s.config.ClientSecret,
		RedirectURL:  s.baseURL + "/github/callback",
		Endpoint:     oauth2github.Endpoint,
		Scopes:       []string{"read:user"},
		Log:          s.log,
		OnSuccess: func(w http.ResponseWriter, r *http.Request, token *oauth2.Token) {
			caller := apiauth.Caller(r.Context())
			if caller == nil {
				http.Error(w, "not authenticated", http.StatusUnauthorized)
				return
			}
			if caller.ID == "" {
				http.Error(w, "this operation requires a user identity", http.StatusForbidden)
				return
			}
			ghClient := github.NewClient(nil).WithAuthToken(token.AccessToken)
			ghUser, _, err := ghClient.Users.Get(r.Context(), "")
			if err != nil {
				s.log.Error("failed to fetch GitHub user", "error", err)
				http.Error(w, "failed to fetch GitHub user", http.StatusInternalServerError)
				return
			}
			if err := s.store.LinkGitHubAccount(r.Context(), nil, caller.ID, ghUser.GetID(), ghUser.GetLogin()); err != nil {
				http.Error(w, "failed to link GitHub account", http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/ui/settings", http.StatusFound)
		},
	})
}

// WebhookHandler returns the HTTP handler for GitHub App webhook events.
func (s *Server) WebhookHandler() http.Handler {
	return &webhookserver.GitHubHandler{
		Router:        &eventrouter.Router{Log: s.log, Store: s.store},
		Store:         s.store,
		WebhookSecret: s.config.WebhookSecret,
	}
}
