package oauthflow

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Default TTL values.
const (
	DefaultAuthCodeTTL     = 60 * time.Second
	DefaultRefreshTokenTTL = 30 * 24 * time.Hour
)

// authCodeClaims are the JWT claims for an OAuth authorization code.
type authCodeClaims struct {
	jwt.RegisteredClaims
	TokenType     string `json:"token_type"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	OrgID         int64  `json:"org_id"`
	ClientID      string `json:"client_id"`
	RedirectURI   string `json:"redirect_uri"`
	CodeChallenge string `json:"code_challenge"`
}

func (a *Auth) verifyAuthCode(tokenStr string) (*authCodeClaims, error) {
	pubKey := a.appKey.Public()
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
	if claims.TokenType != "auth_code" {
		return nil, fmt.Errorf("wrong token type: %q", claims.TokenType)
	}
	return claims, nil
}

// refreshTokenClaims are the JWT claims for a refresh token.
type refreshTokenClaims struct {
	jwt.RegisteredClaims
	TokenType string `json:"token_type"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	OrgID     int64  `json:"org_id"`
}

func (a *Auth) signRefreshToken(claims *refreshTokenClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	return token.SignedString(a.appKey)
}

func (a *Auth) verifyRefreshToken(tokenStr string) (*refreshTokenClaims, error) {
	pubKey := a.appKey.Public()
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
	if claims.TokenType != "refresh_token" {
		return nil, fmt.Errorf("wrong token type: %q", claims.TokenType)
	}
	return claims, nil
}
