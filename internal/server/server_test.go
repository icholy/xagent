package server

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
	"gotest.tools/v3/assert"
)

// createTestUser creates a context with an authenticated user backed by a real org.
func createTestUser(t *testing.T, srv *Server) context.Context {
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
	return apiauth.WithUser(t.Context(), &apiauth.UserInfo{ID: userID, OrgID: org.ID})
}

// setupTestServer creates a test server with a clean database.
// Requires TEST_DATABASE_URL environment variable to be set.
func setupTestServer(t *testing.T) *Server {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	// Open the database (this will create and migrate it)
	db, err := store.Open(dsn, true)
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
