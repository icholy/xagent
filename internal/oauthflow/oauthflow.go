package oauthflow

import (
	"crypto/ed25519"
)

// Options configures the OAuth 2.1 authorization server.
type Options struct {
	AppKey  ed25519.PrivateKey
	BaseURL string
}

// Server implements the OAuth 2.1 authorization code flow with PKCE.
type Server struct {
	appKey  ed25519.PrivateKey
	baseURL string
}

// New creates a new OAuth flow server.
func New(opts Options) *Server {
	return &Server{
		appKey:  opts.AppKey,
		baseURL: opts.BaseURL,
	}
}
