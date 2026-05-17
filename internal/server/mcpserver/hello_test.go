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

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	tests := []struct {
		name      string
		method    string
		accept    string
		wantHello bool
	}{
		{
			name:      "browser",
			method:    http.MethodGet,
			accept:    "text/html,application/xhtml+xml,*/*;q=0.8",
			wantHello: true,
		},
		{
			name:      "mcp client",
			method:    http.MethodGet,
			accept:    "application/json, text/event-stream",
			wantHello: false,
		},
		{
			name:      "post",
			method:    http.MethodPost,
			accept:    "text/html",
			wantHello: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/mcp", nil)
			req.Header.Set("Accept", tt.accept)
			rec := httptest.NewRecorder()

			HelloMiddleware(next).ServeHTTP(rec, req)

			if tt.wantHello {
				assert.Equal(t, rec.Code, http.StatusOK)
				assert.Assert(t, strings.Contains(rec.Body.String(), "xagent MCP server"))
			} else {
				assert.Equal(t, rec.Code, http.StatusTeapot)
			}
		})
	}
}
