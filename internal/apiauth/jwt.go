package apiauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// AppClaims contains the JWT claims for an app-issued token.
type AppClaims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	Name  string `json:"name"`
	OrgID int64  `json:"org_id"`
	Role  string `json:"role,omitempty"`
}

// AppTokenTTL is the default time-to-live for app JWTs.
const AppTokenTTL = 5 * time.Minute

// NewAppClaims creates AppClaims from a UserInfo.
func NewAppClaims(user *UserInfo) *AppClaims {
	now := time.Now()
	return &AppClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AppTokenTTL)),
		},
		Email: user.Email,
		Name:  user.Name,
		OrgID: user.OrgID,
	}
}

// CreateAppPrivateKey generates a new Ed25519 private key for signing app JWTs.
func CreateAppPrivateKey() (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return priv, nil
}

// SignAppToken creates a JWT signed with the Ed25519 private key.
func SignAppToken(key ed25519.PrivateKey, claims *AppClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	return token.SignedString(key)
}

// VerifyAppToken verifies and parses an app JWT, returning the claims.
func VerifyAppToken(key ed25519.PrivateKey, tokenStr string) (*AppClaims, error) {
	pubKey := key.Public()
	token, err := jwt.ParseWithClaims(tokenStr, &AppClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := token.Claims.(*AppClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}
