package apiauth

import (
	"context"
	"net/http"
	"strings"

	httphelper "github.com/zitadel/oidc/v3/pkg/http"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/zitadel-go/v3/pkg/authentication"
	openid "github.com/zitadel/zitadel-go/v3/pkg/authentication/oidc"
	"github.com/zitadel/zitadel-go/v3/pkg/authorization"
	"github.com/zitadel/zitadel-go/v3/pkg/authorization/oauth"
	"github.com/zitadel/zitadel-go/v3/pkg/http/middleware"
	"github.com/zitadel/zitadel-go/v3/pkg/zitadel"
)

// UserInfo contains authenticated user information.
type UserInfo struct {
	ID    string
	Email string
	Name  string
}

type userInfoKey struct{}

// User returns the authenticated user from context.
func User(ctx context.Context) *UserInfo {
	u, _ := ctx.Value(userInfoKey{}).(*UserInfo)
	return u
}

// WithUser returns a context with the user set.
func WithUser(ctx context.Context, user *UserInfo) context.Context {
	return context.WithValue(ctx, userInfoKey{}, user)
}

// Config holds the configuration for ZITADEL authentication.
type Config struct {
	Domain        string
	ClientID      string
	ClientSecret  string
	RedirectURI   string
	PostLogoutURI string
	EncryptionKey []byte
	Scopes        []string
}

// Auth provides hybrid authentication supporting both cookie-based sessions
// (for web UI) and Bearer tokens (for API calls).
type Auth struct {
	// cookie middleware
	cookie *authentication.Interceptor[*openid.DefaultContext]
	// bearer token middleware
	bearer *middleware.Interceptor[*oauth.IntrospectionContext]
	// Handler handles /auth/* routes (login, callback, logout)
	Handler http.Handler
}

// New creates a new Auth instance with both cookie and bearer token support.
func New(ctx context.Context, cfg Config) (*Auth, error) {
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}
	}
	authN, err := authentication.New(ctx,
		// my zitadel instance domain
		zitadel.New(cfg.Domain),
		// not sure?
		string(cfg.EncryptionKey),
		// Initialize cookie-based authentication (for web UI)
		openid.WithCodeFlow[*openid.DefaultContext](openid.ClientIDSecretAuthentication(
			// credentials to access our zitadel instance
			cfg.ClientID,
			cfg.ClientSecret,
			// where to redirect the browswer after login
			cfg.RedirectURI,
			// what jwt claims we want
			cfg.Scopes,
			// used to store the encrypted token & refresh token & state in cookies
			httphelper.NewCookieHandler(cfg.EncryptionKey, cfg.EncryptionKey),
		)),
		// tell zitadel where to redirect to after logout
		authentication.WithPostLogoutRedirectURI[*openid.DefaultContext](cfg.PostLogoutURI),
	)
	if err != nil {
		return nil, err
	}
	// Initialize Bearer token authorization (for API calls)
	// Uses local JWT validation - tokens are validated by checking signature against JWKS
	authZ, err := authorization.New(ctx,
		zitadel.New(cfg.Domain),
		oauth.DefaultJWTAuthorization(cfg.ClientID),
	)
	if err != nil {
		return nil, err
	}
	return &Auth{
		cookie:  authentication.Middleware(authN),
		bearer:  middleware.New(authZ),
		Handler: authN,
	}, nil
}

// RequireAuth returns middleware that requires either a valid Bearer token
// or an authenticated cookie session.
func (a *Auth) RequireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				a.bearer.RequireAuthorization()(next).ServeHTTP(w, r)
				return
			}
			a.cookie.RequireAuthentication()(next).ServeHTTP(w, r)
		})
	}
}

// User returns the authenticated user's information.
// Works with both Bearer token and cookie authentication.
func (a *Auth) User(r *http.Request) *UserInfo {
	if ctx := a.bearer.Context(r.Context()); ctx != nil {
		return &UserInfo{
			ID:    ctx.Subject,
			Email: ctx.Email,
			Name:  ctx.Name,
		}
	}
	if ctx := a.cookie.Context(r.Context()); ctx != nil {
		return &UserInfo{
			ID:    ctx.UserInfo.Subject,
			Email: ctx.UserInfo.Email,
			Name:  ctx.UserInfo.Name,
		}
	}
	return nil
}
