package mcpserver

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed hello.html
var helloHTML []byte

// HelloMiddleware intercepts browser navigations to the MCP endpoint and
// serves a human-readable page explaining what the endpoint is for. MCP
// clients (which advertise application/json or text/event-stream) and any
// non-GET requests pass through to next unchanged.
//
// Without this, a user pasting the MCP URL into a browser address bar sees
// a raw 401 / JSON blob and assumes the server is broken.
func HelloMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isBrowserRequest(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(helloHTML)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isBrowserRequest(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	accept := r.Header.Get("Accept")
	if !strings.Contains(accept, "text/html") {
		return false
	}
	if strings.Contains(accept, "application/json") || strings.Contains(accept, "text/event-stream") {
		return false
	}
	return true
}
