package server

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store"
	"gotest.tools/v3/assert"
)

// testOrg holds the resources created for a test org.
type testOrg struct {
	UserID string
	OrgID  int64
}

// testOrgOptions configures createTestOrg behavior.
type testOrgOptions struct {
	Workspaces bool
}

// createTestOrg creates a user, org, and authenticated context.
func createTestOrg(t *testing.T, srv *Server, opts testOrgOptions) (context.Context, *testOrg) {
	t.Helper()
	userID := uuid.NewString()
	err := srv.store.CreateUser(t.Context(), nil, &model.User{
		ID:    userID,
		Email: userID + "@test.com",
		Name:  "Test User",
	})
	assert.NilError(t, err)
	org := &model.Org{
		Name:  "test-org-" + userID,
		Owner: userID,
	}
	err = srv.store.CreateOrg(t.Context(), nil, org)
	assert.NilError(t, err)
	err = srv.store.AddOrgMember(t.Context(), nil, &model.OrgMember{
		OrgID:  org.ID,
		UserID: userID,
		Role:   "owner",
	})
	assert.NilError(t, err)
	err = srv.store.UpdateDefaultOrgID(t.Context(), nil, userID, org.ID)
	assert.NilError(t, err)
	ctx := apiauth.WithUser(t.Context(), &apiauth.UserInfo{ID: userID, OrgID: org.ID})
	if opts.Workspaces {
		for _, runner := range []string{"test-runner", "runner-1", "runner-2"} {
			_, err = srv.RegisterWorkspaces(ctx, &xagentv1.RegisterWorkspacesRequest{
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
	return ctx, &testOrg{UserID: userID, OrgID: org.ID}
}

// setupTestServer creates a test server with a clean database.
// Requires TEST_DATABASE_URL environment variable to be set.
func setupTestServer(t *testing.T) *Server {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	// Open the database (migrations are run by 'mise run migrate' before tests)
	db, err := store.Open(dsn, false)
	assert.NilError(t, err)

	// Clean up the database when the test completes
	t.Cleanup(func() {
		db.Close()
	})

	// Create and return the server
	return New(Options{
		Store: store.New(db),
	})
}
