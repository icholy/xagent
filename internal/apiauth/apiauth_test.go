package apiauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gotest.tools/v3/assert"
)

func TestSessionCookieMaxAge(t *testing.T) {
	tests := []struct {
		name       string
		maxAge     int
		setCookies []*http.Cookie
		wantMaxAge []int // expected Max-Age for each cookie, -2 means "check unchanged"
	}{
		{
			name:   "adds Max-Age to session cookie",
			maxAge: 2592000,
			setCookies: []*http.Cookie{
				{Name: "zitadel.session", Value: "encrypted", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode},
			},
			wantMaxAge: []int{2592000},
		},
		{
			name:   "does not modify non-session cookies",
			maxAge: 2592000,
			setCookies: []*http.Cookie{
				{Name: "other", Value: "value", Path: "/"},
			},
			wantMaxAge: []int{0},
		},
		{
			name:   "preserves deletion cookie (MaxAge -1 serializes as Max-Age=0)",
			maxAge: 2592000,
			setCookies: []*http.Cookie{
				{Name: "zitadel.session", Value: "", Path: "/", MaxAge: -1},
			},
			// Go's http.SetCookie serializes MaxAge:-1 as "Max-Age=0" in the header.
			// http.Response.Cookies() parses "Max-Age=0" back as MaxAge:-1.
			// Our middleware leaves cookies alone if they already have a Max-Age attribute.
			wantMaxAge: []int{-1},
		},
		{
			name:   "handles multiple cookies",
			maxAge: 86400,
			setCookies: []*http.Cookie{
				{Name: "zitadel.session", Value: "encrypted", Path: "/", HttpOnly: true, Secure: true},
				{Name: "other", Value: "value"},
			},
			wantMaxAge: []int{86400, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, c := range tt.setCookies {
					http.SetCookie(w, c)
				}
				w.WriteHeader(http.StatusFound)
			})

			auth := &Auth{sessionMaxAge: tt.maxAge}
			handler := auth.sessionCookieMaxAge(inner)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/auth/callback", nil)
			handler.ServeHTTP(rec, req)

			resp := rec.Result()
			cookies := resp.Cookies()
			assert.Equal(t, len(cookies), len(tt.wantMaxAge), "cookie count mismatch")
			for i, c := range cookies {
				assert.Equal(t, c.MaxAge, tt.wantMaxAge[i], "cookie %q MaxAge", c.Name)
			}
		})
	}
}

func TestSessionCookieMaxAgeImplicitWrite(t *testing.T) {
	// Test that cookies are patched even when WriteHeader is not explicitly called
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:  "zitadel.session",
			Value: "encrypted",
			Path:  "/",
		})
		w.Write([]byte("OK"))
	})

	auth := &Auth{sessionMaxAge: 2592000}
	handler := auth.sessionCookieMaxAge(inner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth/callback", nil)
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	cookies := resp.Cookies()
	assert.Equal(t, len(cookies), 1)
	assert.Equal(t, cookies[0].MaxAge, 2592000)
}
