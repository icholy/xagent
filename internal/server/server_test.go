package server

import (
	"context"
	"path/filepath"
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"
)

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

func TestGetTask(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()

	// Create a task directly in the database
	task := &store.Task{
		Name:      "Test Task",
		Workspace: "test-workspace",
		Instructions: []store.Instruction{
			{
				Text: "Do something important",
				URL:  "https://example.com/issue/1",
			},
			{
				Text: "Do something else",
				URL:  "https://example.com/issue/2",
			},
		},
		Status: store.TaskStatusPending,
	}
	err := srv.tasks.Create(task)
	assert.NilError(t, err)

	// Get the task via the API
	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{
		Id: task.ID,
	})
	assert.NilError(t, err)

	// Verify the task matches what we created
	expected := &xagentv1.Task{
		Id:        task.ID,
		Name:      "Test Task",
		Parent:    0,
		Workspace: "test-workspace",
		Instructions: []*xagentv1.Instruction{
			{
				Text: "Do something important",
				Url:  "https://example.com/issue/1",
			},
			{
				Text: "Do something else",
				Url:  "https://example.com/issue/2",
			},
		},
		Status:    "pending",
		CreatedAt: getResp.Task.CreatedAt, // Copy timestamps since we can't predict them
		UpdatedAt: getResp.Task.UpdatedAt,
	}

	assert.DeepEqual(t, getResp.Task, expected, protocmp.Transform())
}
