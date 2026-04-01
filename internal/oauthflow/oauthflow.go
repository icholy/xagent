package oauthflow

import (
	"cmp"
	"crypto/ed25519"
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
	baseURL         string
	authCodeTTL     time.Duration
	refreshTokenTTL time.Duration
}

// New creates a new OAuth flow handler.
func New(opts Options) *Auth {
	return &Auth{
		appKey:          opts.AppKey,
		baseURL:         opts.BaseURL,
		authCodeTTL:     cmp.Or(opts.AuthCodeTTL, DefaultAuthCodeTTL),
		refreshTokenTTL: cmp.Or(opts.RefreshTokenTTL, DefaultRefreshTokenTTL),
	}
}
