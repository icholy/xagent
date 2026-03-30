package testserver

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/server"
	"github.com/icholy/xagent/internal/store"
	"gotest.tools/v3/assert"
)

// TestOrg holds the resources created for a test org.
type TestOrg struct {
	UserID string
	OrgID  int64
}

// Options configures CreateOrg behavior.
type Options struct {
	Workspaces bool
}

// Create creates a test server with a clean database.
// Requires TEST_DATABASE_URL environment variable to be set.
func Create(t *testing.T) *server.Server {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := store.Open(dsn, false)
	assert.NilError(t, err)

	t.Cleanup(func() {
		db.Close()
	})

	return server.New(server.Options{
		Store: store.New(db),
	})
}

// CreateOrg creates a user, org, and authenticated context.
func CreateOrg(t *testing.T, srv *server.Server, opts Options) (context.Context, *TestOrg) {
	t.Helper()
	s := srv.Store()
	userID := uuid.NewString()
	err := s.CreateUser(t.Context(), nil, &model.User{
		ID:    userID,
		Email: userID + "@test.com",
		Name:  "Test User",
	})
	assert.NilError(t, err)
	org := &model.Org{
		Name:  "test-org-" + userID,
		Owner: userID,
	}
	err = s.CreateOrg(t.Context(), nil, org)
	assert.NilError(t, err)
	err = s.AddOrgMember(t.Context(), nil, &model.OrgMember{
		OrgID:  org.ID,
		UserID: userID,
		Role:   "owner",
	})
	assert.NilError(t, err)
	err = s.UpdateDefaultOrgID(t.Context(), nil, userID, org.ID)
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
	return ctx, &TestOrg{UserID: userID, OrgID: org.ID}
}
