package server

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
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

func TestCreateAndGetTask(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()

	// Create a task
	createReq := &xagentv1.CreateTaskRequest{
		Name:      "Test Task",
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
	}

	createResp, err := srv.CreateTask(ctx, createReq)
	assert.NilError(t, err)
	assert.Assert(t, createResp != nil)
	assert.Assert(t, createResp.Task != nil)
	assert.Assert(t, createResp.Task.Id > 0, "expected task ID to be set")

	taskID := createResp.Task.Id

	// Get the task back
	getReq := &xagentv1.GetTaskRequest{
		Id: taskID,
	}

	getResp, err := srv.GetTask(ctx, getReq)
	assert.NilError(t, err)
	assert.Assert(t, getResp != nil)
	assert.Assert(t, getResp.Task != nil)

	// Verify the task matches what we created
	expected := &xagentv1.Task{
		Id:        taskID,
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

	if diff := cmp.Diff(expected, getResp.Task, protocmp.Transform()); diff != "" {
		t.Errorf("task mismatch (-want +got):\n%s", diff)
	}
}
