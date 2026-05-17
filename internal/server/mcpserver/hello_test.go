package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func TestHelloMiddleware(t *testing.T) {
	t.Parallel()

	const nextStatus = http.StatusTeapot
	const nextBody = "passed through"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(nextStatus)
		_, _ = w.Write([]byte(nextBody))
	})

	tests := []struct {
		name      string
		method    string
		accept    string
		wantHello bool
	}{
		{
			name:      "browser navigation",
			method:    http.MethodGet,
			accept:    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
			wantHello: true,
		},
		{
			name:      "html only",
			method:    http.MethodGet,
			accept:    "text/html",
			wantHello: true,
		},
		{
			name:      "mcp client",
			method:    http.MethodGet,
			accept:    "application/json, text/event-stream",
			wantHello: false,
		},
		{
			name:      "html plus json (some clients)",
			method:    http.MethodGet,
			accept:    "text/html, application/json",
			wantHello: false,
		},
		{
			name:      "html plus event stream",
			method:    http.MethodGet,
			accept:    "text/html, text/event-stream",
			wantHello: false,
		},
		{
			name:      "no accept header",
			method:    http.MethodGet,
			accept:    "",
			wantHello: false,
		},
		{
			name:      "wildcard accept",
			method:    http.MethodGet,
			accept:    "*/*",
			wantHello: false,
		},
		{
			name:      "post request from browser",
			method:    http.MethodPost,
			accept:    "text/html",
			wantHello: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			handler := HelloMiddleware(next)
			req := httptest.NewRequest(tt.method, "/mcp", nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			rec := httptest.NewRecorder()

			// Act
			handler.ServeHTTP(rec, req)

			// Assert
			if tt.wantHello {
				assert.Equal(t, rec.Code, http.StatusOK)
				assert.Equal(t, rec.Header().Get("Content-Type"), "text/html; charset=utf-8")
				assert.Assert(t, strings.Contains(rec.Body.String(), "xagent MCP server"))
			} else {
				assert.Equal(t, rec.Code, nextStatus)
				assert.Equal(t, rec.Body.String(), nextBody)
			}
		})
	}
}
