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

	// Create a task using the API
	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
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
	})
	assert.NilError(t, err)

	// Get the task via the API
	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{
		Id: createResp.Task.Id,
	})
	assert.NilError(t, err)

	// Verify the task matches what we created
	expected := &xagentv1.Task{
		Id:        createResp.Task.Id,
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

func TestCreateTask(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()

	// Act
	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "New Task",
		Workspace: "test-workspace",
		Instructions: []*xagentv1.Instruction{
			{
				Text: "Do something",
				Url:  "https://example.com/issue/1",
			},
		},
	})

	// Assert
	assert.NilError(t, err)
	expected := &xagentv1.Task{
		Id:        resp.Task.Id,
		Name:      "New Task",
		Parent:    0,
		Workspace: "test-workspace",
		Instructions: []*xagentv1.Instruction{
			{
				Text: "Do something",
				Url:  "https://example.com/issue/1",
			},
		},
		Status:    "pending",
		CreatedAt: resp.Task.CreatedAt,
		UpdatedAt: resp.Task.UpdatedAt,
	}
	assert.DeepEqual(t, resp.Task, expected, protocmp.Transform())
}

func TestListTasks(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()
	_, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task 1",
		Workspace: "workspace-1",
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task 2",
		Workspace: "workspace-2",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListTasks(ctx, &xagentv1.ListTasksRequest{})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Tasks), 2)
}

func TestUpdateTask(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()
	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Original Name",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:   createResp.Task.Id,
		Name: "Updated Name",
	})

	// Assert
	assert.NilError(t, err)
	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Task.Name, "Updated Name")
}

func TestDeleteTask(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()
	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task to Delete",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.DeleteTask(ctx, &xagentv1.DeleteTaskRequest{
		Id: createResp.Task.Id,
	})

	// Assert
	assert.NilError(t, err)
	_, getErr := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	assert.ErrorContains(t, getErr, "")
}

func TestCreateLink(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with Link",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    taskResp.Task.Id,
		Relevance: "Related PR",
		Url:       "https://github.com/example/repo/pull/123",
		Title:     "Fix bug",
		Notify:    true,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, resp.Link.TaskId, taskResp.Task.Id)
	assert.Equal(t, resp.Link.Url, "https://github.com/example/repo/pull/123")
	assert.Equal(t, resp.Link.Notify, true)
}

func TestUploadAndListLogs(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := context.Background()
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with Logs",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
		TaskId: taskResp.Task.Id,
		Entries: []*xagentv1.LogEntry{
			{Type: "info", Content: "First log entry"},
			{Type: "error", Content: "Second log entry"},
		},
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListLogs(ctx, &xagentv1.ListLogsRequest{
		TaskId: taskResp.Task.Id,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Entries), 2)
	assert.Equal(t, resp.Entries[0].Content, "First log entry")
	assert.Equal(t, resp.Entries[1].Content, "Second log entry")
}

func TestSubmitRunnerEvents(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()

	// Create a task and set it to running using SubmitRunnerEvents with direct status update
	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Test Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	taskID := createResp.Task.Id

	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{
				TaskId: taskID,
				Status: "running",
			},
		},
	})
	assert.NilError(t, err)

	// Submit a stopped event
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{
				TaskId:  taskID,
				Event:   "stopped",
				Version: 0,
			},
		},
	})
	assert.NilError(t, err)

	// Verify task status was updated
	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Task.Status, "completed")
}
