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
	"github.com/icholy/zitadel-go/v3/pkg/authentication"
	openid "github.com/icholy/zitadel-go/v3/pkg/authentication/oidc"
	"github.com/icholy/zitadel-go/v3/pkg/authorization"
	"github.com/icholy/zitadel-go/v3/pkg/authorization/oauth"
	"github.com/icholy/zitadel-go/v3/pkg/http/middleware"
	"github.com/icholy/zitadel-go/v3/pkg/zitadel"
	httphelper "github.com/zitadel/oidc/v3/pkg/http"
	"github.com/zitadel/oidc/v3/pkg/oidc"
)

// Auth type constants matching X-Auth-Type header values.
const (
	AuthTypeKey    = "key"
	AuthTypeApp    = "app"
	AuthTypeBearer = "bearer"
	AuthTypeCookie = "cookie"
)

// UserInfo contains authenticated user information.
type UserInfo struct {
	ID    string
	Email string
	Name  string
	OrgID int64
	Type  string // Auth type: one of AuthType* constants
}

// DisplayName returns the best available display name for the user.
func (u *UserInfo) DisplayName() string {
	if u.Name != "" {
		return u.Name
	}
	if u.Email != "" {
		return u.Email
	}
	return u.ID
}

// AuditName returns the display name annotated with the auth type for audit logs.
func (u *UserInfo) AuditName() string {
	name := u.DisplayName()
	if u.Type == AuthTypeKey {
		return name + " (API key)"
	}
	return name
}

type userInfoKey struct{}

// Caller returns the authenticated user from context.
func Caller(ctx context.Context) *UserInfo {
	u, _ := ctx.Value(userInfoKey{}).(*UserInfo)
	return u
}

// MustCaller returns the authenticated user from context, panicking if not present.
func MustCaller(ctx context.Context) *UserInfo {
	u := Caller(ctx)
	if u == nil {
		panic("no UserInfo in request context")
	}
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

// UserResolver provisions users on login and resolves orgs for token issuance.
type UserResolver interface {
	// Provision creates the user and their default org on first login.
	// Called from the OIDC callback via WithOnAuthenticated.
	Provision(ctx context.Context, user *UserInfo) error
	// ResolveOrg resolves the org for token issuance.
	// orgID is the requested org from the query param, or 0 to use the user's default.
	// Returns the resolved org ID or an error if the user is not a member.
	ResolveOrg(ctx context.Context, userID string, orgID int64) (int64, error)
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
	// UserResolver provisions users on login and resolves orgs for token issuance.
	UserResolver UserResolver
	// AppKey is the Ed25519 private key for signing/verifying app JWTs.
	// If nil, a new key is generated on startup.
	AppKey ed25519.PrivateKey
	// DevUser, when set, bypasses SSO authentication.
	// All cookie-based requests are treated as this user.
	// API key and app JWT authentication are unaffected.
	DevUser *UserInfo
}

// Auth provides hybrid authentication supporting both cookie-based sessions
// (for web UI) and Bearer tokens (for API calls).
type Auth struct {
	// devUser, when set, bypasses SSO for cookie-based requests
	devUser *UserInfo
	// cookie middleware
	cookie *authentication.Interceptor[*openid.DefaultContext]
	// bearer token middleware
	bearer *middleware.Interceptor[*oauth.IntrospectionContext]
	// validator validates xat_ API keys
	validator KeyValidator
	// resolver provisions users and resolves orgs on token issuance
	resolver UserResolver
	// handler handles /auth/* routes (login, callback, logout)
	handler http.Handler
	// appKey is the Ed25519 private key for signing/verifying app JWTs
	appKey ed25519.PrivateKey
}

// New creates a new Auth instance with both cookie and bearer token support.
// If cfg.DevUser is set, SSO is bypassed and cookie-based requests use that user.
// API key and app JWT authentication are always available.
func New(ctx context.Context, cfg Config) (*Auth, error) {
	appKey := cfg.AppKey
	if appKey == nil {
		var err error
		appKey, err = CreateAppPrivateKey()
		if err != nil {
			return nil, err
		}
	}
	if cfg.DevUser != nil {
		return &Auth{
			devUser:   cfg.DevUser,
			validator: cfg.KeyValidator,
			resolver:  cfg.UserResolver,
			handler:   http.NotFoundHandler(),
			appKey:    appKey,
		}, nil
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
		// provision user and default org on first login
		authentication.WithOnAuthenticated(func(ctx context.Context, authCtx *openid.DefaultContext) error {
			return cfg.UserResolver.Provision(ctx, &UserInfo{
				ID:    authCtx.UserInfo.Subject,
				Email: authCtx.UserInfo.Email,
				Name:  authCtx.UserInfo.Name,
			})
		}),
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
		cookie:    authentication.Middleware(authN),
		bearer:    middleware.New(authZ),
		validator: cfg.KeyValidator,
		resolver:  cfg.UserResolver,
		handler:   authN,
		appKey:    appKey,
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
		Type:  AuthTypeApp,
	}, nil
}

// useDevUser serves the request as the dev user if one is configured.
// Returns true if the request was handled.
func (a *Auth) useDevUser(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
	if a.devUser == nil {
		return false
	}
	r = r.WithContext(WithUser(r.Context(), a.devUser))
	next.ServeHTTP(w, r)
	return true
}

// RequireAuth returns middleware that requires either a valid Bearer token
// or an authenticated cookie session.
func (a *Auth) RequireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Header.Get(AuthTypeHeader) {
			case AuthTypeKey:
				user, err := a.validateKey(r)
				if err != nil || user == nil {
					http.Error(w, "invalid API key", http.StatusUnauthorized)
					return
				}
				r = r.WithContext(WithUser(r.Context(), user))
				next.ServeHTTP(w, r)
			case AuthTypeApp:
				user, err := a.validateAppToken(r)
				if err != nil || user == nil {
					http.Error(w, "invalid app token", http.StatusUnauthorized)
					return
				}
				r = r.WithContext(WithUser(r.Context(), user))
				next.ServeHTTP(w, r)
			case AuthTypeBearer:
				if !a.useDevUser(w, r, next) {
					a.bearer.RequireAuthorization()(next).ServeHTTP(w, r)
				}
			default:
				if !a.useDevUser(w, r, next) {
					// Try app JWT from Bearer header before falling back to cookie auth.
					// This is needed for OAuth clients (e.g. Claude.ai) that send
					// app JWTs as Bearer tokens without the X-Auth-Type header.
					// TODO: find a cleaner way to handle this.
					if user, err := a.validateAppToken(r); err == nil && user != nil {
						r = r.WithContext(WithUser(r.Context(), user))
						next.ServeHTTP(w, r)
						return
					}
					a.cookie.RequireAuthentication()(next).ServeHTTP(w, r)
				}
			}
		})
	}
}

// CheckAuth returns middleware that checks for valid authentication and
// populates context, but does not reject unauthenticated requests.
func (a *Auth) CheckAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Header.Get(AuthTypeHeader) {
			case AuthTypeKey:
				user, err := a.validateKey(r)
				if err == nil && user != nil {
					r = r.WithContext(WithUser(r.Context(), user))
				}
				next.ServeHTTP(w, r)
			case AuthTypeApp:
				user, err := a.validateAppToken(r)
				if err == nil && user != nil {
					r = r.WithContext(WithUser(r.Context(), user))
				}
				next.ServeHTTP(w, r)
			case AuthTypeBearer:
				if !a.useDevUser(w, r, next) {
					a.bearer.CheckAuthorization()(next).ServeHTTP(w, r)
				}
			default:
				if !a.useDevUser(w, r, next) {
					a.cookie.CheckAuthentication()(next).ServeHTTP(w, r)
				}
			}
		})
	}
}

// RequireUserInterceptor returns a Connect interceptor that fails if UserInfo is not in the context.
func RequireUserInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if Caller(ctx) == nil {
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

// HandleToken returns an HTTP handler for GET /auth/token that issues app JWTs.
// The endpoint is authenticated via cookie session or bearer token.
func (a *Auth) HandleToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user := a.User(r)
		if user == nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		// Parse org_id from query parameter
		var orgID int64
		if raw := r.URL.Query().Get("org_id"); raw != "" {
			var err error
			orgID, err = strconv.ParseInt(raw, 10, 64)
			if err != nil {
				http.Error(w, "invalid org_id", http.StatusBadRequest)
				return
			}
		}
		// Resolve org membership
		if a.resolver != nil {
			resolved, err := a.resolver.ResolveOrg(r.Context(), user.ID, orgID)
			if err != nil {
				slog.Error("failed to resolve org", "error", err, "user_id", user.ID)
				http.Error(w, "failed to resolve org", http.StatusForbidden)
				return
			}
			user.OrgID = resolved
		}
		claims := NewAppClaims(user)
		token, err := SignAppToken(a.appKey, claims)
		if err != nil {
			http.Error(w, "failed to sign token", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token":      token,
			"org_id":     user.OrgID,
			"expires_at": claims.ExpiresAt.Time.Unix(),
		})
	}
}

// cookieUser returns user info from the cookie session only.
func (a *Auth) cookieUser(r *http.Request) *UserInfo {
	if a.devUser != nil {
		return a.devUser
	}
	if ctx := a.cookie.Context(r.Context()); ctx != nil {
		return &UserInfo{
			ID:    ctx.UserInfo.Subject,
			Email: ctx.UserInfo.Email,
			Name:  ctx.UserInfo.Name,
			Type:  AuthTypeCookie,
		}
	}
	return nil
}

// Handler returns the HTTP handler for auth routes (login, callback, logout).
func (a *Auth) Handler() http.Handler {
	return a.handler
}

// User returns the authenticated user's information.
// Works with API keys, Bearer tokens, and cookie authentication.
func (a *Auth) User(r *http.Request) *UserInfo {
	// If UserInfo already set (e.g., by API key or app token in CheckAuth), return it
	if user := Caller(r.Context()); user != nil {
		return user
	}
	switch r.Header.Get(AuthTypeHeader) {
	case AuthTypeBearer:
		if ctx := a.bearer.Context(r.Context()); ctx != nil {
			return &UserInfo{
				ID:    ctx.Subject,
				Email: ctx.Email,
				Name:  ctx.Name,
				Type:  AuthTypeBearer,
			}
		}
		return nil
	case AuthTypeApp:
		// App tokens are validated and set in CheckAuth/RequireAuth
		return nil
	}
	if ctx := a.cookie.Context(r.Context()); ctx != nil {
		return &UserInfo{
			ID:    ctx.UserInfo.Subject,
			Email: ctx.UserInfo.Email,
			Name:  ctx.UserInfo.Name,
			Type:  AuthTypeCookie,
		}
	}
	return nil
}
