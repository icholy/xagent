package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestUploadLogs(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	ctx, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with Logs",
		Runner:    "test-runner",
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
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	ctxA, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	taskResp, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.UploadLogs(ctxB, &xagentv1.UploadLogsRequest{
		TaskId: taskResp.Task.Id,
		Entries: []*xagentv1.LogEntry{
			{Type: "info", Content: "Sneaky log"},
		},
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestListLogs(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	ctx, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with Logs",
		Runner:    "test-runner",
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
	assert.Equal(t, len(resp.Entries), 3)
	assert.Equal(t, resp.Entries[0].Type, "audit")
	assert.Equal(t, resp.Entries[1].Content, "First log entry")
	assert.Equal(t, resp.Entries[2].Content, "Second log entry")
}

func TestListLogs_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	ctxA, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	taskResp, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.UploadLogs(ctxA, &xagentv1.UploadLogsRequest{
		TaskId: taskResp.Task.Id,
		Entries: []*xagentv1.LogEntry{
			{Type: "info", Content: "User A's log"},
		},
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListLogs(ctxB, &xagentv1.ListLogsRequest{
		TaskId: taskResp.Task.Id,
	})

	// Assert - User B gets empty list, not an error
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Entries), 0)
}
