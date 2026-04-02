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

// setupTestServer creates a test server with a clean database.
// Requires TEST_DATABASE_URL environment variable to be set.
func setupTestServer(t *testing.T) *Server {
	t.Helper()
	return New(Options{
		Store: teststore.New(t),
	})
}
