package server

import (
	"context"
	"testing"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/store/teststore"
)

// createTestOrg creates a user, org, and authenticated context.
func createTestOrg(t *testing.T, srv *Server, opts *teststore.OrgOptions) (context.Context, *teststore.Org) {
	t.Helper()
	org := teststore.CreateOrg(t, srv.store, opts)
	ctx := apiauth.WithUser(t.Context(), &apiauth.UserInfo{ID: org.UserID, OrgID: org.OrgID})
	return ctx, org
}
