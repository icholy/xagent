// Package teststore provides helpers for setting up a store in tests.
package teststore

import (
	"os"
	"testing"

	"github.com/icholy/xagent/internal/store"
)

// New creates a *store.Store connected to the test database.
// It skips the test if TEST_DATABASE_URL is not set and registers
// a cleanup function to close the database connection.
func New(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := store.Open(dsn, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
	})
	return store.New(db)
}
