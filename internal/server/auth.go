package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
)

// AuthConfig holds configuration for OAuth/OIDC authentication.
type AuthConfig struct {
	IssuerURL    string // OIDC issuer URL (e.g., https://your-instance.zitadel.cloud)
	ClientID     string
	ClientSecret string
}

// Auth handles OAuth/OIDC authentication using Bearer tokens.
type Auth struct {
	log      *slog.Logger
	config   *AuthConfig
	verifier *oidc.IDTokenVerifier
	provider *oidc.Provider
	users    *store.UserRepository
}

// NewAuth creates a new Auth handler.
func NewAuth(ctx context.Context, log *slog.Logger, config *AuthConfig, users *store.UserRepository) (*Auth, error) {
	provider, err := oidc.NewProvider(ctx, config.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: config.ClientID})

	return &Auth{
		log:      log,
		config:   config,
		verifier: verifier,
		provider: provider,
		users:    users,
	}, nil
}

// Handler returns an http.Handler for auth routes.
func (a *Auth) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/me", a.handleMe)
	return mux
}

func (a *Auth) handleMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := UserFromContext(ctx)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user.Proto())
}

// ValidateToken validates a Bearer token and returns the user info.
// It expects the token to be an OIDC ID token.
func (a *Auth) ValidateToken(ctx context.Context, token string) (*model.User, error) {
	// Verify the ID token
	idToken, err := a.verifier.Verify(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("failed to verify token: %w", err)
	}

	// Extract claims
	var claims struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	// Find or create user
	user, err := a.users.GetBySubject(ctx, nil, claims.Sub)
	if errors.Is(err, sql.ErrNoRows) {
		// Create new user
		user = &model.User{
			Subject: claims.Sub,
			Email:   claims.Email,
			Name:    claims.Name,
			Picture: claims.Picture,
		}
		if err := a.users.Create(ctx, nil, user); err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		a.log.Info("user created", "id", user.ID, "email", user.Email)
	} else if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	} else {
		// Update user info if changed
		if user.Email != claims.Email || user.Name != claims.Name || user.Picture != claims.Picture {
			user.Email = claims.Email
			user.Name = claims.Name
			user.Picture = claims.Picture
			if err := a.users.Put(ctx, nil, user); err != nil {
				a.log.Error("failed to update user", "error", err)
				// Not fatal, continue
			}
		}
	}

	return user, nil
}

// ExtractBearerToken extracts the Bearer token from the Authorization header.
func ExtractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
