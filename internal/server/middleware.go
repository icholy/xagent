package server

import (
	"context"
	"net/http"

	"github.com/icholy/xagent/internal/model"
)

type contextKey string

const userContextKey contextKey = "user"

// UserFromContext returns the user from the context, or nil if not present.
func UserFromContext(ctx context.Context) *model.User {
	user, _ := ctx.Value(userContextKey).(*model.User)
	return user
}

// ContextWithUser returns a new context with the user.
func ContextWithUser(ctx context.Context, user *model.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// AuthMiddleware creates HTTP middleware that validates session and adds user to context.
func AuthMiddleware(auth *Auth) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			session, err := auth.GetSession(ctx, r)
			if err != nil {
				auth.log.Error("failed to get session", "error", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			if session == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			user, err := auth.GetUser(ctx, session)
			if err != nil {
				auth.log.Error("failed to get user", "error", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			if user == nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			ctx = ContextWithUser(ctx, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
