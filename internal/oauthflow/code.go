package oauthflow

import (
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const (
	authCodeTTL      = 60 * time.Second
	refreshTokenTTL  = 30 * 24 * time.Hour
)

// authCodeClaims are the JWT claims for an OAuth authorization code.
type authCodeClaims struct {
	jwt.RegisteredClaims
	Email         string `json:"email"`
	Name          string `json:"name"`
	OrgID         int64  `json:"org_id"`
	ClientID      string `json:"client_id"`
	RedirectURI   string `json:"redirect_uri"`
	CodeChallenge string `json:"code_challenge"`
}

// refreshTokenClaims are the JWT claims for a refresh token.
type refreshTokenClaims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	Name  string `json:"name"`
	OrgID int64  `json:"org_id"`
}

// signAuthCode signs an authorization code JWT.
func signAuthCode(key ed25519.PrivateKey, claims *authCodeClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	return token.SignedString(key)
}

// verifyAuthCode verifies and parses an authorization code JWT.
func verifyAuthCode(key ed25519.PrivateKey, tokenStr string) (*authCodeClaims, error) {
	pubKey := key.Public()
	token, err := jwt.ParseWithClaims(tokenStr, &authCodeClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse auth code: %w", err)
	}
	claims, ok := token.Claims.(*authCodeClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid auth code claims")
	}
	return claims, nil
}

// signRefreshToken signs a refresh token JWT.
func signRefreshToken(key ed25519.PrivateKey, claims *refreshTokenClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	return token.SignedString(key)
}

// verifyRefreshToken verifies and parses a refresh token JWT.
func verifyRefreshToken(key ed25519.PrivateKey, tokenStr string) (*refreshTokenClaims, error) {
	pubKey := key.Public()
	token, err := jwt.ParseWithClaims(tokenStr, &refreshTokenClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse refresh token: %w", err)
	}
	claims, ok := token.Claims.(*refreshTokenClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid refresh token claims")
	}
	return claims, nil
}
