package server

import (
	"context"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/store"
	"gotest.tools/v3/assert"
)

// randomUserID creates a context with an authenticated user for testing.
func randomUserID(t *testing.T) context.Context {
	t.Helper()
	id := uuid.NewString()
	orgID := rand.Int64N(1<<53) + 1
	return apiauth.WithUser(t.Context(), &apiauth.UserInfo{ID: id, OrgID: orgID})
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
