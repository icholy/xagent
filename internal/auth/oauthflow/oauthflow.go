package oauthflow

import (
	"cmp"
	"crypto/ed25519"
	"fmt"
	"net/url"
	"time"
)

// Options configures the OAuth 2.1 authorization flow.
type Options struct {
	AppKey          ed25519.PrivateKey
	BaseURL         string
	AuthCodeTTL     time.Duration
	RefreshTokenTTL time.Duration
}

// Auth implements the OAuth 2.1 authorization code flow with PKCE.
type Auth struct {
	appKey          ed25519.PrivateKey
	baseURL         *url.URL
	authCodeTTL     time.Duration
	refreshTokenTTL time.Duration
}

// New creates a new OAuth flow handler.
func New(opts Options) (*Auth, error) {
	u, err := url.Parse(opts.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	return &Auth{
		appKey:          opts.AppKey,
		baseURL:         u,
		authCodeTTL:     cmp.Or(opts.AuthCodeTTL, DefaultAuthCodeTTL),
		refreshTokenTTL: cmp.Or(opts.RefreshTokenTTL, DefaultRefreshTokenTTL),
	}, nil
}
