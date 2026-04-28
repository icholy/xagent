package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/deviceauth"
	"github.com/icholy/xagent/internal/auth/oauthflow"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/server/apiserver"
	"github.com/icholy/xagent/internal/server/atlassianserver"
	"github.com/icholy/xagent/internal/server/githubserver"
	"github.com/icholy/xagent/internal/server/notifyserver"
	"github.com/icholy/xagent/internal/server/mcpserver"
	"github.com/icholy/xagent/internal/store"
	"github.com/icholy/xagent/internal/x/otelx"
	"github.com/justinas/alice"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Server struct {
	log       *slog.Logger
	api       *apiserver.Server
	auth      *apiauth.Auth
	discovery deviceauth.DiscoveryConfig
	github    *githubserver.Server
	atlassian *atlassianserver.Server
	baseURL   string
	oauth     *oauthflow.Auth
	cors      bool
	notify    *notifyserver.Server
}

type Options struct {
	Log           *slog.Logger
	Store         *store.Store
	Auth          *apiauth.Auth
	Discovery     deviceauth.DiscoveryConfig
	GitHub        *githubserver.Server
	Atlassian     *atlassianserver.Server
	BaseURL       string
	EncryptionKey []byte
	OAuth         *oauthflow.Auth
	CORS          bool
	Publisher     pubsub.Publisher
	Notify        *notifyserver.Server
}

func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	api := apiserver.New(apiserver.Options{
		Log:       log,
		Store:     opts.Store,
		BaseURL:   opts.BaseURL,
		Publisher: opts.Publisher,
		Atlassian: opts.Atlassian,
		GitHub:    opts.GitHub,
	})
	return &Server{
		log:       log,
		api:       api,
		auth:      opts.Auth,
		discovery: opts.Discovery,
		github:    opts.GitHub,
		atlassian: opts.Atlassian,
		baseURL:   opts.BaseURL,
		oauth:     opts.OAuth,
		cors:      opts.CORS,
		notify:    opts.Notify,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// Device flow discovery endpoint (public)
	mux.HandleFunc(deviceauth.DiscoveryPath, s.handleDeviceConfig)
	// App JWT token endpoint (cookie-authenticated)
	mux.Handle("/auth/token", alice.New(s.auth.CheckAuth()).Then(s.auth.HandleToken()))
	// Auth routes (login, callback, logout)
	mux.Handle("/auth/", s.auth.Handler())
	// Connect RPC API (protected)
	// HTTP middleware checks auth and attaches UserInfo to context
	// Connect interceptor enforces auth with proper RPC error responses
	otelInterceptor, err := otelconnect.NewInterceptor()
	if err != nil {
		s.log.Error("failed to create otelconnect interceptor", "error", err)
	}
	path, handler := xagentv1connect.NewXAgentServiceHandler(s.api,
		connect.WithInterceptors(otelInterceptor, apiauth.RequireUserInterceptor()),
	)
	mux.Handle(path, alice.New(s.auth.CheckAuth(), s.auth.AttachUserInfo()).Then(handler))
	// SSE endpoint (protected)
	if s.notify != nil {
		mux.Handle("/events", alice.New(s.auth.CheckAuth(), s.auth.AttachUserInfo()).Then(s.notify.Handler()))
	}
	// GitHub App routes (conditionally registered)
	if s.github != nil {
		githubAuth := alice.New(s.auth.RequireAuth(), s.auth.AttachUserInfo())
		link := s.github.OAuthLink()
		mux.Handle("/github/login", githubAuth.ThenFunc(link.HandleLogin))
		mux.Handle("/github/callback", githubAuth.ThenFunc(link.HandleCallback))
		mux.Handle("/webhook/github", s.github.WebhookHandler())
	}
	// Atlassian OAuth routes (conditionally registered)
	if s.atlassian != nil {
		atlassianAuth := alice.New(s.auth.RequireAuth(), s.auth.AttachUserInfo())
		link := s.atlassian.OAuthLink()
		mux.Handle("/atlassian/login", atlassianAuth.ThenFunc(link.HandleLogin))
		mux.Handle("/atlassian/callback", atlassianAuth.ThenFunc(link.HandleCallback))
		mux.Handle("/webhook/atlassian", s.atlassian.WebhookHandler())
	}
	// OAuth 2.1 endpoints (public, conditionally registered)
	if s.oauth != nil {
		mux.HandleFunc("/.well-known/oauth-authorization-server", s.oauth.HandleMetadata)
		mux.HandleFunc("/.well-known/oauth-protected-resource", s.oauth.HandleResourceMetadata)
		mux.HandleFunc("/oauth/register", s.oauth.HandleRegister)
		mux.HandleFunc("/oauth/authorize", s.oauth.HandleAuthorize)
		mux.HandleFunc("/oauth/token", s.oauth.HandleToken)
	}
	// MCP endpoint (protected by auth middleware)
	mux.Handle("/mcp", alice.New(s.auth.RequireAuth(), s.auth.AttachUserInfo()).Then(mcpserver.New(s.api, s.baseURL).Handler()))
	// React UI (SPA with client-side routing, protected by cookie auth)
	mux.Handle("/ui/", http.StripPrefix("/ui", s.auth.RequireAuth()(WebUI())))
	mux.Handle("/", http.RedirectHandler("/ui/", http.StatusFound))
	return otelhttp.NewHandler(otelx.TraceResponseHeader(s.handleCORS(mux)), "xagent")
}

// handleCORS adds permissive CORS headers to all responses when CORS is enabled.
func (s *Server) handleCORS(next http.Handler) http.Handler {
	if !s.cors {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version")
		w.Header().Set("Access-Control-Expose-Headers", "Traceresponse")
		if r.Method == http.MethodOptions {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleDeviceConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.discovery)
}
