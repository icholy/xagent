package apiauth

import "net/http"

// WithTestUser returns an http.Handler that injects user into the request
// context before delegating to next. Use it in tests to exercise handlers
// that call MustCaller without setting up the full auth middleware chain.
func WithTestUser(next http.Handler, user *UserInfo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}
