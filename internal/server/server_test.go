package server

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/icholy/xagent/internal/apiauth"
	"github.com/icholy/xagent/internal/store"
	"gotest.tools/v3/assert"
)

// withUserID creates a context with an authenticated user for testing.
func withUserID(t *testing.T, id string) context.Context {
	t.Helper()
	return apiauth.WithUser(t.Context(), &apiauth.UserInfo{ID: id})
}

// setupTestServer creates a test server with a clean database in a temporary directory.
// The database is automatically cleaned up when the test completes.
func setupTestServer(t *testing.T) *Server {
	t.Helper()

	// Create a temporary directory for the test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Open the database (this will create and migrate it)
	db, err := store.Open(dbPath)
	assert.NilError(t, err)

	// Clean up the database when the test completes
	t.Cleanup(func() {
		db.Close()
	})

	// Create repositories
	tasks := store.NewTaskRepository(db)
	logs := store.NewLogRepository(db)
	links := store.NewLinkRepository(db)
	events := store.NewEventRepository(db)

	// Create and return the server
	return New(Options{
		Tasks:  tasks,
		Logs:   logs,
		Links:  links,
		Events: events,
	})
}
