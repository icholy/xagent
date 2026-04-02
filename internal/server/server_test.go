package server

import (
	"context"
	"testing"

	"github.com/icholy/xagent/internal/apiauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// testOrgOptions configures createTestOrg behavior.
type testOrgOptions struct {
	Workspaces bool
}

// createTestOrg creates a user, org, and authenticated context.
func createTestOrg(t *testing.T, srv *Server, opts testOrgOptions) (context.Context, *teststore.Org) {
	t.Helper()
	org := teststore.CreateOrg(t, srv.store)
	ctx := apiauth.WithUser(t.Context(), &apiauth.UserInfo{ID: org.UserID, OrgID: org.OrgID})
	if opts.Workspaces {
		for _, runner := range []string{"test-runner", "runner-1", "runner-2"} {
			_, err := srv.RegisterWorkspaces(ctx, &xagentv1.RegisterWorkspacesRequest{
				RunnerId: runner,
				Workspaces: []*xagentv1.RegisteredWorkspace{
					{Name: "test-workspace"},
					{Name: "workspace-1"},
					{Name: "workspace-2"},
					{Name: "default"},
				},
			})
			assert.NilError(t, err)
		}
	}
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
