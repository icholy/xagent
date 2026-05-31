package agentauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"slices"

	"github.com/golang-jwt/jwt/v5"
)

// Scopes grant tasks additional capabilities beyond their own task data.
const (
	// ScopeGitHubToken allows issuing GitHub App installation tokens via the
	// CreateGitHubToken RPC.
	ScopeGitHubToken = "github_token"
	// ScopeChildTasks allows creating, listing, updating, and reading logs of
	// child tasks.
	ScopeChildTasks = "child_tasks"
)

// ValidScope reports whether scope is a recognized capability scope.
func ValidScope(scope string) bool {
	switch scope {
	case ScopeGitHubToken, ScopeChildTasks:
		return true
	default:
		return false
	}
}

// TaskClaims contains the JWT claims for a task's identity.
type TaskClaims struct {
	jwt.RegisteredClaims
	TaskID    int64    `json:"task_id"`
	Workspace string   `json:"workspace"`
	Runner    string   `json:"runner"`
	Scopes    []string `json:"scopes,omitempty"`
}

// HasScope reports whether the claims include the given capability scope.
func (c *TaskClaims) HasScope(scope string) bool {
	return slices.Contains(c.Scopes, scope)
}

// CreatePrivateKey generates a new Ed25519 private key.
func CreatePrivateKey() (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return priv, nil
}

// SignToken creates a JWT signed with the private key.
func SignToken(key ed25519.PrivateKey, claims *TaskClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	return token.SignedString(key)
}

// VerifyToken verifies and parses a JWT, returning the claims.
func VerifyToken(key ed25519.PrivateKey, tokenStr string) (*TaskClaims, error) {
	// Ed25519 public key is derived from private key
	pubKey := key.Public()
	token, err := jwt.ParseWithClaims(tokenStr, &TaskClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := token.Claims.(*TaskClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}
