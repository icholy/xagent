package apiserver

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestUploadLogs(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)
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

func TestUploadLogs_ReportBecomesEvent(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with report",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act - the agent's report tool uploads an `llm` entry.
	_, err = srv.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
		TaskId: taskResp.Task.Id,
		Entries: []*xagentv1.LogEntry{
			{Type: "llm", Content: "Opened PR #952"},
		},
	})
	assert.NilError(t, err)

	// Assert - the report is a from-agent report event, not a logs row.
	events, err := srv.ListEventsByTask(ctx, &xagentv1.ListEventsByTaskRequest{TaskId: taskResp.Task.Id})
	assert.NilError(t, err)
	var reports []*xagentv1.ReportPayload
	for _, e := range events.Events {
		if r := e.GetReport(); r != nil {
			assert.Equal(t, e.Wake, false)
			reports = append(reports, r)
		}
	}
	assert.Equal(t, len(reports), 1)
	assert.Equal(t, reports[0].Content, "Opened PR #952")

	// The report does not write a logs row (only the create audit log remains).
	logs, err := srv.ListLogs(ctx, &xagentv1.ListLogsRequest{TaskId: taskResp.Task.Id})
	assert.NilError(t, err)
	for _, entry := range logs.Entries {
		assert.Assert(t, entry.Type != "llm", "report must not write an llm log row")
	}
}

func TestUploadLogs_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)
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
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)
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
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)
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

	// Assert - User B gets empty list, not an error (blanket read skips the row
	// load; the list query is org-scoped).
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Entries), 0)
}
