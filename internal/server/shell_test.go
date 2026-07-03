package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"gotest.tools/v3/assert"
)

// requestWithCaller builds a request carrying caller in its context, standing in
// for what the Bearer auth middleware injects in production. A nil caller leaves
// the context empty.
func requestWithCaller(caller *apiauth.UserInfo) *http.Request {
	ctx := context.Background()
	if caller != nil {
		ctx = apiauth.WithUser(ctx, caller)
	}
	return httptest.NewRequest(http.MethodGet, "/shell/s1/attach", nil).WithContext(ctx)
}

func TestAuthorizeShellAttach_MatchingOrg(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := requestWithCaller(&apiauth.UserInfo{ID: "u", OrgID: 7})

	assert.Assert(t, authorizeShellAttach(rec, req, 7))
	assert.Equal(t, rec.Code, http.StatusOK) // untouched: allowed requests aren't written to.
}

func TestAuthorizeShellAttach_DifferentOrg(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := requestWithCaller(&apiauth.UserInfo{ID: "u", OrgID: 7})

	assert.Assert(t, !authorizeShellAttach(rec, req, 8))
	assert.Equal(t, rec.Code, http.StatusForbidden)
}

func TestAuthorizeShellAttach_NoCaller(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := requestWithCaller(nil)

	assert.Assert(t, !authorizeShellAttach(rec, req, 7))
	assert.Equal(t, rec.Code, http.StatusUnauthorized)
}
