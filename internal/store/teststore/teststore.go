// Package teststore provides helpers for setting up a store in tests.
package teststore

import (
	"cmp"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/model"
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

// Org holds the user and org created by CreateOrg.
type Org struct {
	UserID string
	OrgID  int64
}

// TaskOptions configures CreateTask behavior. Zero values are replaced
// with sensible defaults.
type TaskOptions struct {
	Status    model.TaskStatus
	Workspace string
}

// CreateTask creates a task in the given org.
func CreateTask(t *testing.T, s *store.Store, org *Org, opts *TaskOptions) *model.Task {
	t.Helper()
	if opts == nil {
		opts = &TaskOptions{}
	}
	task := &model.Task{
		OrgID:     org.OrgID,
		Status:    cmp.Or(opts.Status, model.TaskStatusPending),
		Workspace: cmp.Or(opts.Workspace, "default"),
	}
	if err := s.CreateTask(t.Context(), nil, task); err != nil {
		t.Fatal(err)
	}
	return task
}

// CreateOrg creates a user, org, org membership, and sets the default org.
func CreateOrg(t *testing.T, s *store.Store) *Org {
	t.Helper()
	ctx := t.Context()
	userID := uuid.NewString()
	err := s.CreateUser(ctx, nil, &model.User{
		ID:    userID,
		Email: userID + "@test.com",
		Name:  "Test User",
	})
	if err != nil {
		t.Fatal(err)
	}
	org := &model.Org{
		Name:  "test-org-" + userID,
		Owner: userID,
	}
	if err := s.CreateOrg(ctx, nil, org); err != nil {
		t.Fatal(err)
	}
	if err := s.AddOrgMember(ctx, nil, &model.OrgMember{
		OrgID:  org.ID,
		UserID: userID,
		Role:   "owner",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDefaultOrgID(ctx, nil, userID, org.ID); err != nil {
		t.Fatal(err)
	}
	return &Org{
		UserID: userID,
		OrgID:  org.ID,
	}
}
