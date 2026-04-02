package server

import (
	"context"
	"testing"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/store/teststore"
)

var defaultWorkspaces = &teststore.OrgOptions{
	Workspaces: []teststore.WorkspaceOptions{
		{RunnerID: "test-runner", Name: "test-workspace"},
		{RunnerID: "test-runner", Name: "workspace-1"},
		{RunnerID: "test-runner", Name: "workspace-2"},
		{RunnerID: "test-runner", Name: "default"},
		{RunnerID: "runner-1", Name: "test-workspace"},
		{RunnerID: "runner-1", Name: "workspace-1"},
		{RunnerID: "runner-1", Name: "workspace-2"},
		{RunnerID: "runner-1", Name: "default"},
		{RunnerID: "runner-2", Name: "test-workspace"},
		{RunnerID: "runner-2", Name: "workspace-1"},
		{RunnerID: "runner-2", Name: "workspace-2"},
		{RunnerID: "runner-2", Name: "default"},
	},
}

// createTestOrg creates a user, org, and authenticated context with default workspaces.
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
