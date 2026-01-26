package agentauth

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"strings"
)

type contextKey string

const claimsContextKey contextKey = "agent-claims"

// ClaimsFromContext retrieves the TaskClaims from the context.
func ClaimsFromContext(ctx context.Context) (*TaskClaims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*TaskClaims)
	return claims, ok
}

// ContextWithClaims returns a new context with the given claims.
func ContextWithClaims(ctx context.Context, claims *TaskClaims) context.Context {
	return context.WithValue(ctx, claimsContextKey, claims)
}

// Middleware extracts and validates the agent JWT from the Authorization header.
// It stores the verified claims in the request context for use by handlers.
func Middleware(key ed25519.PrivateKey) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}

			tokenStr, ok := strings.CutPrefix(auth, "Bearer ")
			if !ok {
				http.Error(w, "invalid authorization header", http.StatusUnauthorized)
				return
			}

			claims, err := VerifyToken(key, tokenStr)
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TokenSource provides access tokens for authentication.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// StaticTokenSource returns a fixed token.
type StaticTokenSource string

func (s StaticTokenSource) Token(ctx context.Context) (string, error) {
	return string(s), nil
}
