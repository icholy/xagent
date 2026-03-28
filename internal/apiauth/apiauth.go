package apiauth

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"connectrpc.com/connect"
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
	OrgID int64
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

// KeyValidator validates API keys and returns the associated user.
type KeyValidator interface {
	ValidateKey(ctx context.Context, keyHash string) (*UserInfo, error)
}

// UserProvisioner is called to provision or update a user record on login.
type UserProvisioner interface {
	ProvisionUser(ctx context.Context, user *UserInfo) error
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
	// KeyValidator validates xat_ API keys.
	KeyValidator KeyValidator
	// UserProvisioner is called on login to ensure the user exists in the database.
	UserProvisioner UserProvisioner
	// AppKey is the Ed25519 private key for signing/verifying app JWTs.
	// If nil, a new key is generated on startup.
	AppKey ed25519.PrivateKey
	// Disable authentication (for development only).
	// When true, all requests are authenticated as a default "dev" user.
	Disable bool
}

// Auth provides hybrid authentication supporting both cookie-based sessions
// (for web UI) and Bearer tokens (for API calls).
type Auth struct {
	// disabled indicates auth is disabled (all requests get a default user)
	disabled bool
	// cookie middleware
	cookie *authentication.Interceptor[*openid.DefaultContext]
	// bearer token middleware
	bearer *middleware.Interceptor[*oauth.IntrospectionContext]
	// validator validates xat_ API keys
	validator KeyValidator
	// provisioner provisions users on login
	provisioner UserProvisioner
	// handler handles /auth/* routes (login, callback, logout)
	handler http.Handler
	// appKey is the Ed25519 private key for signing/verifying app JWTs
	appKey ed25519.PrivateKey
}

// New creates a new Auth instance with both cookie and bearer token support.
// If cfg.Disable is true, authentication is bypassed and all requests get a default user.
func New(ctx context.Context, cfg Config) (*Auth, error) {
	appKey := cfg.AppKey
	if appKey == nil {
		var err error
		appKey, err = CreateAppPrivateKey()
		if err != nil {
			return nil, err
		}
	}
	if cfg.Disable {
		return &Auth{disabled: true, appKey: appKey}, nil
	}
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
		// store session in cookie instead of in-memory (survives server restarts)
		authentication.WithCookieSession[*openid.DefaultContext](),
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
		cookie:      authentication.Middleware(authN),
		bearer:      middleware.New(authZ),
		validator:   cfg.KeyValidator,
		provisioner: cfg.UserProvisioner,
		handler:     authN,
		appKey:      appKey,
	}, nil
}

// AuthTypeHeader is the header CLI/device clients send to indicate bearer auth.
const AuthTypeHeader = "X-Auth-Type"

// validateKey extracts and validates an API key from the Authorization header.
func (a *Auth) validateKey(r *http.Request) (*UserInfo, error) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return nil, errors.New("missing Bearer token")
	}
	return a.validator.ValidateKey(r.Context(), HashKey(raw))
}

// validateAppToken extracts and validates an app JWT from the Authorization header.
func (a *Auth) validateAppToken(r *http.Request) (*UserInfo, error) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return nil, errors.New("missing Bearer token")
	}
	claims, err := VerifyAppToken(a.appKey, raw)
	if err != nil {
		return nil, err
	}
	return &UserInfo{
		ID:    claims.Subject,
		Email: claims.Email,
		Name:  claims.Name,
		OrgID: claims.OrgID,
	}, nil
}

// RequireAuth returns middleware that requires either a valid Bearer token
// or an authenticated cookie session.
func (a *Auth) RequireAuth() func(http.Handler) http.Handler {
	if a.disabled {
		return a.attachDefaultUser()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Header.Get(AuthTypeHeader) {
			case "key":
				user, err := a.validateKey(r)
				if err != nil || user == nil {
					http.Error(w, "invalid API key", http.StatusUnauthorized)
					return
				}
				r = r.WithContext(WithUser(r.Context(), user))
				next.ServeHTTP(w, r)
			case "app":
				user, err := a.validateAppToken(r)
				if err != nil || user == nil {
					http.Error(w, "invalid app token", http.StatusUnauthorized)
					return
				}
				r = r.WithContext(WithUser(r.Context(), user))
				next.ServeHTTP(w, r)
			case "bearer":
				a.bearer.RequireAuthorization()(next).ServeHTTP(w, r)
			default:
				a.cookie.RequireAuthentication()(next).ServeHTTP(w, r)
			}
		})
	}
}

// CheckAuth returns middleware that checks for valid authentication and
// populates context, but does not reject unauthenticated requests.
func (a *Auth) CheckAuth() func(http.Handler) http.Handler {
	if a.disabled {
		return a.attachDefaultUser()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Header.Get(AuthTypeHeader) {
			case "key":
				user, err := a.validateKey(r)
				if err == nil && user != nil {
					r = r.WithContext(WithUser(r.Context(), user))
				}
				next.ServeHTTP(w, r)
			case "app":
				user, err := a.validateAppToken(r)
				if err == nil && user != nil {
					r = r.WithContext(WithUser(r.Context(), user))
				}
				next.ServeHTTP(w, r)
			case "bearer":
				a.bearer.CheckAuthorization()(next).ServeHTTP(w, r)
			default:
				a.cookie.CheckAuthentication()(next).ServeHTTP(w, r)
			}
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
	if a.disabled {
		return a.attachDefaultUser()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if user := a.User(r); user != nil {
				r = r.WithContext(WithUser(r.Context(), user))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// HandleToken returns an HTTP handler for GET /auth/token that issues app JWTs.
// The endpoint is authenticated via cookie session.
func (a *Auth) HandleToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user := a.cookieUser(r)
		if user == nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		// Provision user on login
		if a.provisioner != nil {
			if err := a.provisioner.ProvisionUser(r.Context(), user); err != nil {
				slog.Error("failed to provision user", "error", err, "user_id", user.ID)
			}
		}
		// Parse org_id from query parameter (defaults to 0)
		if raw := r.URL.Query().Get("org_id"); raw != "" {
			orgID, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				http.Error(w, "invalid org_id", http.StatusBadRequest)
				return
			}
			user.OrgID = orgID
		}
		claims := NewAppClaims(user)
		token, err := SignAppToken(a.appKey, claims)
		if err != nil {
			http.Error(w, "failed to sign token", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token":     token,
			"org_id":    user.OrgID,
			"expires_at": claims.ExpiresAt.Time.Unix(),
		})
	}
}

// cookieUser returns user info from the cookie session only.
func (a *Auth) cookieUser(r *http.Request) *UserInfo {
	if a.disabled {
		return &UserInfo{
			ID:    "dev",
			Email: "dev@localhost",
			Name:  "Developer",
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

// Handler returns the HTTP handler for auth routes (login, callback, logout).
func (a *Auth) Handler() http.Handler {
	if a.disabled {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}
	return a.handler
}

// attachDefaultUser returns middleware that attaches a default user to all requests.
func (a *Auth) attachDefaultUser() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := &UserInfo{
				ID:    "dev",
				Email: "dev@localhost",
				Name:  "Developer",
			}
			r = r.WithContext(WithUser(r.Context(), user))
			next.ServeHTTP(w, r)
		})
	}
}

// User returns the authenticated user's information.
// Works with API keys, Bearer tokens, and cookie authentication.
func (a *Auth) User(r *http.Request) *UserInfo {
	// If UserInfo already set (e.g., by API key or app token in CheckAuth), return it
	if user := User(r.Context()); user != nil {
		return user
	}
	switch r.Header.Get(AuthTypeHeader) {
	case "bearer":
		if ctx := a.bearer.Context(r.Context()); ctx != nil {
			return &UserInfo{
				ID:    ctx.Subject,
				Email: ctx.Email,
				Name:  ctx.Name,
			}
		}
		return nil
	case "app":
		// App tokens are validated and set in CheckAuth/RequireAuth
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
