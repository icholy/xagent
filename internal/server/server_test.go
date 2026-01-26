package server

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/store"
	"gotest.tools/v3/assert"
)

// randomUserID creates a context with an authenticated user for testing.
// If id is empty, a random UUID is generated.
func randomUserID(t *testing.T) context.Context {
	t.Helper()
	id := uuid.NewString()
	return apiauth.WithUser(t.Context(), &apiauth.UserInfo{ID: id})
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
