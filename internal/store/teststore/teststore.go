// Package teststore provides helpers for setting up a store in tests.
package teststore

import (
	"cmp"
	"os"
	"testing"
	"time"

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

// LinkOptions configures a link created with a task.
type LinkOptions struct {
	URL       string
	Title     string
	Subscribe bool
}

// TaskOptions configures CreateTask behavior. Zero values are replaced
// with sensible defaults.
type TaskOptions struct {
	Status    model.TaskStatus
	Workspace string
	Links     []LinkOptions
}

// CreateTask creates a task in the given org, along with any links.
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
	for _, lo := range opts.Links {
		link := &model.Link{
			TaskID:    task.ID,
			URL:       cmp.Or(lo.URL, "https://example.com"),
			Title:     cmp.Or(lo.Title, "test link"),
			Subscribe: lo.Subscribe,
			CreatedAt: time.Now(),
		}
		if err := s.CreateLink(t.Context(), nil, link); err != nil {
			t.Fatal(err)
		}
	}
	return task
}

// WorkspaceOptions configures a workspace created with an org.
type WorkspaceOptions struct {
	Name     string
	RunnerID string
}

// OrgOptions configures CreateOrg behavior. Zero values are replaced
// with sensible defaults.
type OrgOptions struct {
	Workspaces []WorkspaceOptions
}

// CreateOrg creates a user, org, org membership, and sets the default org.
func CreateOrg(t *testing.T, s *store.Store, opts *OrgOptions) *Org {
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
	result := &Org{
		UserID: userID,
		OrgID:  org.ID,
	}
	if opts != nil {
		for _, ws := range opts.Workspaces {
			if err := s.CreateWorkspace(ctx, nil,
				cmp.Or(ws.RunnerID, "test-runner"),
				cmp.Or(ws.Name, "default"),
				"",
				org.ID,
			); err != nil {
				t.Fatal(err)
			}
		}
	}
	return result
}
