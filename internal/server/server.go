package server

import (
	"crypto/ed25519"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/oauthflow"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/server/apiserver"
	"github.com/icholy/xagent/internal/server/atlassianserver"
	"github.com/icholy/xagent/internal/server/githubserver"
	"github.com/icholy/xagent/internal/server/mcpserver"
	"github.com/icholy/xagent/internal/server/notifyserver"
	"github.com/icholy/xagent/internal/server/shellserver"
	"github.com/icholy/xagent/internal/shell/shellrelay"
	"github.com/icholy/xagent/internal/store"
	"github.com/icholy/xagent/internal/x/otelx"
	"github.com/justinas/alice"
)

type Server struct {
	log       *slog.Logger
	api       *apiserver.Server
	auth      *apiauth.Auth
	github    *githubserver.Server
	atlassian *atlassianserver.Server
	baseURL   string
	oauth     *oauthflow.Auth
	cors      bool
	notify    *notifyserver.Server
	shell     *shellserver.Registry
}

type Options struct {
	Log           *slog.Logger
	Store         *store.Store
	Auth          *apiauth.Auth
	GitHub        *githubserver.Server
	Atlassian     *atlassianserver.Server
	BaseURL       string
	EncryptionKey []byte
	OAuth         *oauthflow.Auth
	CORS          bool
	Publisher     pubsub.Publisher
	Notify        *notifyserver.Server
	AppKey        ed25519.PrivateKey
}

func New(opts Options) *Server {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	shell := shellserver.New(log, shellrelay.DefaultEstablishTimeout)
	apiOpts := apiserver.Options{
		Log:       log,
		Store:     opts.Store,
		BaseURL:   opts.BaseURL,
		Publisher: opts.Publisher,
		Atlassian: opts.Atlassian,
		AppKey:    opts.AppKey,
		Shells:    shell,
	}
	// Only populate GitHub when configured: assigning a typed-nil
	// *githubserver.Server into the interface field would make
	// apiserver's s.github != nil ("configured") check spuriously true.
	if opts.GitHub != nil {
		apiOpts.GitHub = opts.GitHub
	}
	api := apiserver.New(apiOpts)
	return &Server{
		log:       log,
		api:       api,
		auth:      opts.Auth,
		github:    opts.GitHub,
		atlassian: opts.Atlassian,
		baseURL:   opts.BaseURL,
		oauth:     opts.OAuth,
		cors:      opts.CORS,
		notify:    opts.Notify,
		shell:     shell,
	}
}

func (s *Server) Handler() http.Handler {
	mux := otelx.NewMux("xagent")
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
	mux.Handle(path, alice.New(s.auth.CheckAuth()).Then(handler))
	// SSE endpoint (protected)
	if s.notify != nil {
		mux.Handle("/events", alice.New(s.auth.CheckAuth()).Then(s.notify.Handler()))
	}
	// GitHub App routes (conditionally registered)
	if s.github != nil {
		link := s.github.OAuthLink()
		mux.Handle("/github/login", alice.New(s.auth.RequireAuth()).ThenFunc(link.HandleLogin))
		mux.Handle("/github/callback", alice.New(s.auth.RequireAuth()).ThenFunc(link.HandleCallback))
		mux.Handle("/webhook/github", s.github.WebhookHandler())
	}
	// Atlassian OAuth routes (conditionally registered)
	if s.atlassian != nil {
		link := s.atlassian.OAuthLink()
		mux.Handle("/atlassian/login", alice.New(s.auth.RequireAuth()).ThenFunc(link.HandleLogin))
		mux.Handle("/atlassian/callback", alice.New(s.auth.RequireAuth()).ThenFunc(link.HandleCallback))
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
	// Shell rendezvous relay (step 2 of the driver reverse shell, #1113).
	// Both legs ride the same Bearer auth as the other authenticated endpoints:
	// the driver leg with its task token, the operator (attach) leg with a Bearer
	// token whose org claim must match the session's owning org.
	mux.Handle("GET /shell/{session}/driver", alice.New(s.auth.RequireAuth()).Then(s.shell.DriverHandler()))
	mux.Handle("GET /shell/{session}/attach", alice.New(s.auth.RequireAuth()).Then(s.shell.AttachHandler()))
	// MCP endpoint (protected by auth middleware)
	mux.Handle("/mcp", alice.New(s.auth.RequireAuth()).Then(mcpserver.Handler(s.api)))
	// React UI (SPA with client-side routing, protected by cookie auth)
	mux.Handle("/ui/", http.StripPrefix("/ui", s.auth.RequireAuth()(WebUI())))
	mux.Handle("/", http.RedirectHandler("/ui/", http.StatusFound))
	return mux.Handler(s.handleCORS)
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
