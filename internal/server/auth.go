package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
	"golang.org/x/oauth2"
)

const (
	sessionCookieName = "xagent_session"
	stateCookieName   = "xagent_oauth_state"
	sessionDuration   = 7 * 24 * time.Hour // 7 days
)

// AuthConfig holds configuration for OAuth/OIDC authentication.
type AuthConfig struct {
	GoogleClientID     string
	GoogleClientSecret string
	BaseURL            string
	SessionSecret      string
}

// Auth handles OAuth/OIDC authentication.
type Auth struct {
	log          *slog.Logger
	config       *AuthConfig
	oauth2Config *oauth2.Config
	verifier     *oidc.IDTokenVerifier
	users        *store.UserRepository
	sessions     *store.SessionRepository
}

// NewAuth creates a new Auth handler.
func NewAuth(ctx context.Context, log *slog.Logger, config *AuthConfig, users *store.UserRepository, sessions *store.SessionRepository) (*Auth, error) {
	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	oauth2Config := &oauth2.Config{
		ClientID:     config.GoogleClientID,
		ClientSecret: config.GoogleClientSecret,
		RedirectURL:  config.BaseURL + "/auth/callback",
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: config.GoogleClientID})

	return &Auth{
		log:          log,
		config:       config,
		oauth2Config: oauth2Config,
		verifier:     verifier,
		users:        users,
		sessions:     sessions,
	}, nil
}

// Handler returns an http.Handler for auth routes.
func (a *Auth) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", a.handleLogin)
	mux.HandleFunc("GET /auth/callback", a.handleCallback)
	mux.HandleFunc("GET /auth/logout", a.handleLogout)
	mux.HandleFunc("GET /auth/me", a.handleMe)
	return mux
}

func (a *Auth) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		http.Error(w, "Failed to generate state", http.StatusInternalServerError)
		return
	}

	// Store state in cookie
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	url := a.oauth2Config.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (a *Auth) handleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Verify state
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil {
		http.Error(w, "Missing state cookie", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:   stateCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	// Exchange code for token
	code := r.URL.Query().Get("code")
	token, err := a.oauth2Config.Exchange(ctx, code)
	if err != nil {
		a.log.Error("failed to exchange code", "error", err)
		http.Error(w, "Failed to exchange code", http.StatusInternalServerError)
		return
	}

	// Extract and verify ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "Missing ID token", http.StatusInternalServerError)
		return
	}

	idToken, err := a.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		a.log.Error("failed to verify ID token", "error", err)
		http.Error(w, "Failed to verify ID token", http.StatusInternalServerError)
		return
	}

	// Extract claims
	var claims struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil {
		a.log.Error("failed to parse claims", "error", err)
		http.Error(w, "Failed to parse claims", http.StatusInternalServerError)
		return
	}

	// Find or create user
	user, err := a.users.GetByGoogleID(ctx, nil, claims.Sub)
	if errors.Is(err, sql.ErrNoRows) {
		// Create new user
		user = &model.User{
			GoogleID: claims.Sub,
			Email:    claims.Email,
			Name:     claims.Name,
			Picture:  claims.Picture,
		}
		if err := a.users.Create(ctx, nil, user); err != nil {
			a.log.Error("failed to create user", "error", err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}
		a.log.Info("user created", "id", user.ID, "email", user.Email)
	} else if err != nil {
		a.log.Error("failed to get user", "error", err)
		http.Error(w, "Failed to get user", http.StatusInternalServerError)
		return
	} else {
		// Update user info
		user.Email = claims.Email
		user.Name = claims.Name
		user.Picture = claims.Picture
		if err := a.users.Put(ctx, nil, user); err != nil {
			a.log.Error("failed to update user", "error", err)
			// Not fatal, continue
		}
	}

	// Create session
	sessionID, err := generateSessionID()
	if err != nil {
		http.Error(w, "Failed to generate session", http.StatusInternalServerError)
		return
	}

	session := &model.Session{
		ID:        sessionID,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(sessionDuration),
	}
	if err := a.sessions.Create(ctx, nil, session); err != nil {
		a.log.Error("failed to create session", "error", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	a.log.Info("user logged in", "user_id", user.ID, "email", user.Email)

	// Redirect to home
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (a *Auth) handleLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		if err := a.sessions.Delete(ctx, nil, cookie.Value); err != nil {
			a.log.Error("failed to delete session", "error", err)
		}
	}

	// Clear session cookie
	http.SetCookie(w, &http.Cookie{
		Name:   sessionCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
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

// GetSession returns the session for the given request, or nil if not authenticated.
func (a *Auth) GetSession(ctx context.Context, r *http.Request) (*model.Session, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, nil
	}

	session, err := a.sessions.Get(ctx, nil, cookie.Value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if session.IsExpired() {
		_ = a.sessions.Delete(ctx, nil, session.ID)
		return nil, nil
	}

	return session, nil
}

// GetUser returns the user for the given session, or nil if not found.
func (a *Auth) GetUser(ctx context.Context, session *model.Session) (*model.User, error) {
	if session == nil {
		return nil, nil
	}
	user, err := a.users.Get(ctx, nil, session.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return user, err
}

func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
