package apiserver

import (
	"testing"
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"gotest.tools/v3/assert"
)

func TestGetTask(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	// Create a task using the API
	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Test Task",
		Runner:    "test-runner",
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

	// Verify the task matches what we created. Instructions are no longer a task
	// field — they are instruction events in the stream, asserted below.
	expected := &xagentv1.Task{
		Id:          createResp.Task.Id,
		Name:        "Test Task",
		Runner:      "test-runner",
		Workspace:   "test-workspace",
		Status:      xagentv1.TaskStatus_PENDING,
		Command:     xagentv1.TaskCommand_START,
		Actions:     &xagentv1.TaskActions{Cancel: true},
		Version:     1,
		CreatedAt:   getResp.Task.CreatedAt, // Copy timestamps since we can't predict them
		UpdatedAt:   getResp.Task.UpdatedAt,
		AutoArchive: durationpb.New(0),
	}

	assert.DeepEqual(t, getResp.Task, expected, protocmp.Transform())

	// The initial instructions are seeded into the stream as instruction events.
	detailsResp, err := srv.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)
	assert.DeepEqual(t, instructionPayloads(detailsResp.Events), []*xagentv1.InstructionPayload{
		{Text: "Do something important", Url: "https://example.com/issue/1"},
		{Text: "Do something else", Url: "https://example.com/issue/2"},
	}, protocmp.Transform())
}

// instructionPayloads extracts the instruction-arm payloads from a stream, in
// order — the brief's instruction projection.
func instructionPayloads(events []*xagentv1.Event) []*xagentv1.InstructionPayload {
	var out []*xagentv1.InstructionPayload
	for _, e := range events {
		if inst := e.GetInstruction(); inst != nil {
			out = append(out, inst)
		}
	}
	return out
}

func TestGetTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)
	createResp, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, errA := srv.GetTask(ctxA, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	_, errB := srv.GetTask(ctxB, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})

	// Assert
	assert.NilError(t, errA)
	assert.ErrorContains(t, errB, "not found")
}

func TestGetTaskDetails_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)
	createResp, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, errA := srv.GetTaskDetails(ctxA, &xagentv1.GetTaskDetailsRequest{Id: createResp.Task.Id})
	_, errB := srv.GetTaskDetails(ctxB, &xagentv1.GetTaskDetailsRequest{Id: createResp.Task.Id})

	// Assert
	assert.NilError(t, errA)
	assert.ErrorContains(t, errB, "not found")
}

func TestCreateTask(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	// Act
	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "New Task",
		Runner:    "test-runner",
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
		Id:          resp.Task.Id,
		Name:        "New Task",
		Runner:      "test-runner",
		Workspace:   "test-workspace",
		Status:      xagentv1.TaskStatus_PENDING,
		Command:     xagentv1.TaskCommand_START,
		Actions:     &xagentv1.TaskActions{Cancel: true},
		Version:     1,
		CreatedAt:   resp.Task.CreatedAt,
		UpdatedAt:   resp.Task.UpdatedAt,
		AutoArchive: durationpb.New(0),
	}
	assert.DeepEqual(t, resp.Task, expected, protocmp.Transform())

	// The initial instruction is seeded into the stream as an instruction event.
	detailsResp, err := srv.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: resp.Task.Id})
	assert.NilError(t, err)
	assert.DeepEqual(t, instructionPayloads(detailsResp.Events), []*xagentv1.InstructionPayload{
		{Text: "Do something", Url: "https://example.com/issue/1"},
	}, protocmp.Transform())
}

func TestListTasks(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)
	_, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task 1",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task 2",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListTasks(ctx, &xagentv1.ListTasksRequest{})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Tasks), 2)
}

func TestListTasks_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)
	_, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task 1",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task 2",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(ctxB, &xagentv1.CreateTaskRequest{
		Name:      "User B's Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	respA, err := srv.ListTasks(ctxA, &xagentv1.ListTasksRequest{})
	assert.NilError(t, err)
	respB, err := srv.ListTasks(ctxB, &xagentv1.ListTasksRequest{})
	assert.NilError(t, err)

	// Assert
	assert.Equal(t, len(respA.Tasks), 2)
	assert.Equal(t, len(respB.Tasks), 1)
}

func TestListTasks_Pagination(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	first, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{Name: "task-1", Runner: "test-runner", Workspace: "test-workspace"})
	assert.NilError(t, err)
	second, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{Name: "task-2", Runner: "test-runner", Workspace: "test-workspace"})
	assert.NilError(t, err)

	// The first page holds the newest task and a token for the next page.
	page1, err := srv.ListTasks(ctx, &xagentv1.ListTasksRequest{PageSize: 1})
	assert.NilError(t, err)
	assert.Equal(t, len(page1.Tasks), 1)
	assert.Equal(t, page1.Tasks[0].Id, second.Task.Id)
	assert.Assert(t, page1.NextPageToken != "")

	// Passing that token back returns the next (older) task and no further pages.
	page2, err := srv.ListTasks(ctx, &xagentv1.ListTasksRequest{PageSize: 1, PageToken: page1.NextPageToken})
	assert.NilError(t, err)
	assert.Equal(t, len(page2.Tasks), 1)
	assert.Equal(t, page2.Tasks[0].Id, first.Task.Id)
	assert.Equal(t, page2.NextPageToken, "")
}

func TestCreateTask_BadRunner(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	_, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Bad Task",
		Runner:    "nonexistent-runner",
		Workspace: "test-workspace",
	})
	assert.ErrorContains(t, err, "not found")
}

func TestCreateTask_BadWorkspace(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	_, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Bad Task",
		Runner:    "test-runner",
		Workspace: "fake-workspace",
	})
	assert.ErrorContains(t, err, "not found")
}

func TestUpdateTask(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)
	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Original Name",
		Runner:    "test-runner",
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

func TestUpdateTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)
	createResp, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.UpdateTask(ctxB, &xagentv1.UpdateTaskRequest{
		Id:   createResp.Task.Id,
		Name: "Hijacked Name",
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestArchiveTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)
	createResp, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.ArchiveTask(ctxB, &xagentv1.ArchiveTaskRequest{
		Id: createResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestCancelTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)
	createResp, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.CancelTask(ctxB, &xagentv1.CancelTaskRequest{
		Id: createResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestRestartTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)
	createResp, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.RestartTask(ctxB, &xagentv1.RestartTaskRequest{
		Id: createResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestCreateTask_AutoArchive(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	want := 90 * time.Minute
	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:        "With archive",
		Runner:      "test-runner",
		Workspace:   "test-workspace",
		AutoArchive: durationpb.New(want),
	})
	assert.NilError(t, err)
	assert.Assert(t, resp.Task.AutoArchive != nil, "AutoArchive should be set on response")
	assert.Equal(t, resp.Task.AutoArchive.AsDuration(), want)

	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: resp.Task.Id})
	assert.NilError(t, err)
	assert.Assert(t, getResp.Task.AutoArchive != nil)
	assert.Equal(t, getResp.Task.AutoArchive.AsDuration(), want)
}

func TestUpdateTask_AutoArchive(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "No archive",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	assert.Equal(t, createResp.Task.AutoArchive.AsDuration(), time.Duration(0), "unset means never auto-archive")

	// Set a value via Update
	want := 24 * time.Hour
	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:          createResp.Task.Id,
		AutoArchive: durationpb.New(want),
	})
	assert.NilError(t, err)
	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)
	assert.Assert(t, getResp.Task.AutoArchive != nil)
	assert.Equal(t, getResp.Task.AutoArchive.AsDuration(), want)

	// Omitting AutoArchive on Update leaves the existing value untouched.
	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:   createResp.Task.Id,
		Name: "just a rename",
	})
	assert.NilError(t, err)
	getResp, err = srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Task.AutoArchive.AsDuration(), want, "omitted AutoArchive preserves existing value")

	// Setting to zero reverts to "never auto-archive" (omitted on the wire).
	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:          createResp.Task.Id,
		AutoArchive: durationpb.New(0),
	})
	assert.NilError(t, err)
	getResp, err = srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Task.AutoArchive.AsDuration(), time.Duration(0), "zero duration means never auto-archive")
}

func TestUnarchiveTask_ClearsAutoArchive(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:        "Auto-archive task",
		Runner:      "test-runner",
		Workspace:   "test-workspace",
		AutoArchive: durationpb.New(time.Hour),
	})
	assert.NilError(t, err)

	// Put the task in a terminal state, then archive it manually.
	dbTask, err := srv.store.GetTask(ctx, nil, createResp.Task.Id, org.OrgID)
	assert.NilError(t, err)
	dbTask.Status = 5 // COMPLETED
	dbTask.Command = 0
	assert.NilError(t, srv.store.UpdateTask(ctx, nil, dbTask))

	_, err = srv.ArchiveTask(ctx, &xagentv1.ArchiveTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)

	// Unarchive should clear auto_archive so the archiver doesn't immediately re-archive.
	_, err = srv.UnarchiveTask(ctx, &xagentv1.UnarchiveTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)

	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)
	assert.Assert(t, !getResp.Task.Archived, "task should be unarchived")
	assert.Equal(t, getResp.Task.AutoArchive.AsDuration(), time.Duration(0), "AutoArchive should reset to 0 (never) on unarchive")
}
