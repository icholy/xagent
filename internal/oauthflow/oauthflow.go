package oauthflow

import (
	"crypto/ed25519"
)

// Options configures the OAuth 2.1 authorization flow.
type Options struct {
	AppKey  ed25519.PrivateKey
	BaseURL string
}

// Auth implements the OAuth 2.1 authorization code flow with PKCE.
type Auth struct {
	appKey  ed25519.PrivateKey
	baseURL string
}

// New creates a new OAuth flow handler.
func New(opts Options) *Auth {
	return &Auth{
		appKey:  opts.AppKey,
		baseURL: opts.BaseURL,
	}
}
