package oauthflow

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	"github.com/icholy/xagent/internal/model"
)

// Store is the subset of *store.Store that oauthflow needs. It's an interface
// so handlers can be unit-tested without a real database.
type Store interface {
	UpsertPendingIntegration(ctx context.Context, tx *sql.Tx, p *model.PendingIntegration) error
	GetPendingIntegration(ctx context.Context, tx *sql.Tx, typ model.PendingIntegrationType, externalID string) (*model.PendingIntegration, error)
}

// Options configures the OAuth 2.1 authorization flow.
type Options struct {
	AppKey          ed25519.PrivateKey
	BaseURL         string
	Store           Store
	AuthCodeTTL     time.Duration
	RefreshTokenTTL time.Duration
}

// Auth implements the OAuth 2.1 authorization code flow with PKCE.
type Auth struct {
	appKey          ed25519.PrivateKey
	baseURL         *url.URL
	store           Store
	authCodeTTL     time.Duration
	refreshTokenTTL time.Duration
}

// New creates a new OAuth flow handler.
func New(opts Options) (*Auth, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("oauthflow: store is required")
	}
	u, err := url.Parse(opts.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	return &Auth{
		appKey:          opts.AppKey,
		baseURL:         u,
		store:           opts.Store,
		authCodeTTL:     cmp.Or(opts.AuthCodeTTL, DefaultAuthCodeTTL),
		refreshTokenTTL: cmp.Or(opts.RefreshTokenTTL, DefaultRefreshTokenTTL),
	}, nil
}
