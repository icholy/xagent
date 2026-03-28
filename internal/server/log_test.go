package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"gotest.tools/v3/assert"
)

func TestUploadLogs(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with Logs",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
		TaskId: taskResp.Task.Id,
		Entries: []*xagentv1.LogEntry{
			{Type: "info", Content: "First log entry"},
			{Type: "error", Content: "Second log entry"},
		},
	})

	// Assert
	assert.NilError(t, err)
}

func TestUploadLogs_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	taskResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.UploadLogs(userB, &xagentv1.UploadLogsRequest{
		TaskId: taskResp.Task.Id,
		Entries: []*xagentv1.LogEntry{
			{Type: "info", Content: "Sneaky log"},
		},
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestListLogs(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)
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

func TestListLogs_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	taskResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.UploadLogs(userA, &xagentv1.UploadLogsRequest{
		TaskId: taskResp.Task.Id,
		Entries: []*xagentv1.LogEntry{
			{Type: "info", Content: "User A's log"},
		},
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListLogs(userB, &xagentv1.ListLogsRequest{
		TaskId: taskResp.Task.Id,
	})

	// Assert - User B gets empty list, not an error
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Entries), 0)
}
