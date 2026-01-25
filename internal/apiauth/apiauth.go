package apiauth

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/zitadel/middlewarex"
	httphelper "github.com/zitadel/oidc/v3/pkg/http"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/zitadel-go/v3/pkg/authentication"
	openid "github.com/zitadel/zitadel-go/v3/pkg/authentication/oidc"
	"github.com/zitadel/zitadel-go/v3/pkg/authorization"
	"github.com/zitadel/zitadel-go/v3/pkg/authorization/oauth"
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
	bearer *middlewarex.Interceptor[*oauth.IntrospectionContext]
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
		bearer:  middlewarex.New(authZ),
		Handler: authN,
	}, nil
}

// AuthTypeHeader is the header CLI/device clients send to indicate bearer auth.
const AuthTypeHeader = "X-Auth-Type"

// RequireAuth returns middleware that requires either a valid Bearer token
// or an authenticated cookie session.
func (a *Auth) RequireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// CLI/device clients send X-Auth-Type: bearer
			if r.Header.Get(AuthTypeHeader) == "bearer" {
				a.bearer.RequireAuthorization()(next).ServeHTTP(w, r)
				return
			}
			// Web clients use cookie auth
			a.cookie.RequireAuthentication()(next).ServeHTTP(w, r)
		})
	}
}

// CheckAuth returns middleware that checks for valid authentication and
// populates context, but does not reject unauthenticated requests.
func (a *Auth) CheckAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// CLI/device clients send X-Auth-Type: bearer
			if r.Header.Get(AuthTypeHeader) == "bearer" {
				a.bearer.CheckAuthorization()(next).ServeHTTP(w, r)
				return
			}
			// Web clients use cookie auth
			a.cookie.CheckAuthentication()(next).ServeHTTP(w, r)
		})
	}
}

// RequireUserInterceptor returns a Connect interceptor that fails if UserInfo is not in the context.
func RequireUserInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if User(ctx) == nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
			}
			return next(ctx, req)
		}
	}
}

// AttachUserInfo extracts user info from auth context and attaches it to the request context.
func (a *Auth) AttachUserInfo() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if user := a.User(r); user != nil {
				r = r.WithContext(WithUser(r.Context(), user))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// User returns the authenticated user's information.
// Works with both Bearer token and cookie authentication.
func (a *Auth) User(r *http.Request) *UserInfo {
	if r.Header.Get(AuthTypeHeader) == "bearer" {
		if ctx := a.bearer.Context(r.Context()); ctx != nil {
			return &UserInfo{
				ID:    ctx.Subject,
				Email: ctx.Email,
				Name:  ctx.Name,
			}
		}
		return nil
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
