package apiserver

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/pubsub"
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

	// Verify the task matches what we created
	expected := &xagentv1.Task{
		Id:        createResp.Task.Id,
		Name:      "Test Task",
		Parent:    0,
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
		Status:       xagentv1.TaskStatus_PENDING,
		Command:      xagentv1.TaskCommand_START,
		Actions:      &xagentv1.TaskActions{Cancel: true},
		Version:      1,
		CreatedAt:    getResp.Task.CreatedAt, // Copy timestamps since we can't predict them
		UpdatedAt:    getResp.Task.UpdatedAt,
		ArchiveAfter: durationpb.New(0),
	}

	assert.DeepEqual(t, getResp.Task, expected, protocmp.Transform())
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
		Id:        resp.Task.Id,
		Name:      "New Task",
		Parent:    0,
		Runner:    "test-runner",
		Workspace: "test-workspace",
		Instructions: []*xagentv1.Instruction{
			{
				Text: "Do something",
				Url:  "https://example.com/issue/1",
			},
		},
		Status:       xagentv1.TaskStatus_PENDING,
		Command:      xagentv1.TaskCommand_START,
		Actions:      &xagentv1.TaskActions{Cancel: true},
		Version:      1,
		CreatedAt:    resp.Task.CreatedAt,
		UpdatedAt:    resp.Task.UpdatedAt,
		ArchiveAfter: durationpb.New(0),
	}
	assert.DeepEqual(t, resp.Task, expected, protocmp.Transform())
}

func TestCreateTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)
	parentResp, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Parent Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.CreateTask(ctxB, &xagentv1.CreateTaskRequest{
		Name:      "User B's Child Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
		Parent:    parentResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
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

func TestListChildTasks_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB := createCtx(t, orgB)
	parentResp, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Parent Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Child Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
		Parent:    parentResp.Task.Id,
	})
	assert.NilError(t, err)

	// Act
	respA, err := srv.ListChildTasks(ctxA, &xagentv1.ListChildTasksRequest{
		ParentId: parentResp.Task.Id,
	})
	assert.NilError(t, err)
	respB, err := srv.ListChildTasks(ctxB, &xagentv1.ListChildTasksRequest{
		ParentId: parentResp.Task.Id,
	})
	assert.NilError(t, err)

	// Assert
	assert.Equal(t, len(respA.Tasks), 1)
	assert.Equal(t, len(respB.Tasks), 0)
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

func TestCreateTask_ArchiveAfter(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	want := 90 * time.Minute
	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:         "With archive",
		Runner:       "test-runner",
		Workspace:    "test-workspace",
		ArchiveAfter: durationpb.New(want),
	})
	assert.NilError(t, err)
	assert.Assert(t, resp.Task.ArchiveAfter != nil, "ArchiveAfter should be set on response")
	assert.Equal(t, resp.Task.ArchiveAfter.AsDuration(), want)

	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: resp.Task.Id})
	assert.NilError(t, err)
	assert.Assert(t, getResp.Task.ArchiveAfter != nil)
	assert.Equal(t, getResp.Task.ArchiveAfter.AsDuration(), want)
}

func TestUpdateTask_ArchiveAfter(t *testing.T) {
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
	assert.Equal(t, createResp.Task.ArchiveAfter.AsDuration(), time.Duration(0), "unset means never auto-archive")

	// Set a value via Update
	want := 24 * time.Hour
	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:           createResp.Task.Id,
		ArchiveAfter: durationpb.New(want),
	})
	assert.NilError(t, err)
	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)
	assert.Assert(t, getResp.Task.ArchiveAfter != nil)
	assert.Equal(t, getResp.Task.ArchiveAfter.AsDuration(), want)

	// Omitting ArchiveAfter on Update leaves the existing value untouched.
	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:   createResp.Task.Id,
		Name: "just a rename",
	})
	assert.NilError(t, err)
	getResp, err = srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Task.ArchiveAfter.AsDuration(), want, "omitted ArchiveAfter preserves existing value")

	// Setting to zero reverts to "never auto-archive" (omitted on the wire).
	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:           createResp.Task.Id,
		ArchiveAfter: durationpb.New(0),
	})
	assert.NilError(t, err)
	getResp, err = srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Task.ArchiveAfter.AsDuration(), time.Duration(0), "zero duration means never auto-archive")
}

func TestUpdateTask_TaskChange_LogAndChannelAgree(t *testing.T) {
	t.Parallel()
	// Both the persisted log row and the published channel message must
	// derive from the same Changed slice — the structural invariant that
	// the TaskChange projection replaces PR #725's hand-written gate.
	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	srv := New(Options{Store: teststore.New(t), Publisher: pub})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{Name: "t", Runner: "r", Workspace: "w"})
	assert.NilError(t, err)
	// Land the task at Completed so UpdateTask{Start: true} actually queues it.
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: createResp.Task.Id, Event: "started", Version: 1},
			{TaskId: createResp.Task.Id, Event: "stopped", Version: 1},
		},
	})
	assert.NilError(t, err)
	pub.ResetCalls()

	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:              createResp.Task.Id,
		Name:            "renamed",
		AddInstructions: []*xagentv1.Instruction{{Text: "do thing"}},
		Start:           true,
	})
	assert.NilError(t, err)

	logs, err := srv.store.ListLogsByTask(ctx, nil, createResp.Task.Id, org.OrgID)
	assert.NilError(t, err)
	var update *model.Log
	for _, l := range logs {
		if strings.Contains(l.Content, "updated task") {
			update = l
			break
		}
	}
	assert.Assert(t, update != nil, "expected an update log row")
	assert.Assert(t, strings.Contains(update.Content, "name"), "log missing 'name': %q", update.Content)
	assert.Assert(t, strings.Contains(update.Content, "instructions"), "log missing 'instructions': %q", update.Content)
	assert.Assert(t, strings.Contains(update.Content, "started"), "log missing 'started': %q", update.Content)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	msg := calls[0].N.ChannelMessage
	assert.Assert(t, strings.Contains(msg, "queued"), "channel message missing 'queued': %q", msg)
	assert.Assert(t, strings.Contains(msg, "name"), "channel message missing 'name': %q", msg)
	assert.Assert(t, strings.Contains(msg, "instructions"), "channel message missing 'instructions': %q", msg)
	assert.Assert(t, strings.Contains(msg, fmt.Sprintf("Task %d", createResp.Task.Id)),
		"channel message missing task id: %q", msg)
}

func TestUpdateTask_NameOnly_LogsButSilent(t *testing.T) {
	t.Parallel()
	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	srv := New(Options{Store: teststore.New(t), Publisher: pub})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{Name: "t", Runner: "r", Workspace: "w"})
	assert.NilError(t, err)
	// Move to Running so PendingRunner == "" and a name-only update stays silent.
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: createResp.Task.Id, Event: "started", Version: 1}},
	})
	assert.NilError(t, err)
	pub.ResetCalls()

	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: createResp.Task.Id, Name: "renamed"})
	assert.NilError(t, err)

	logs, err := srv.store.ListLogsByTask(ctx, nil, createResp.Task.Id, org.OrgID)
	assert.NilError(t, err)
	var update *model.Log
	for _, l := range logs {
		if strings.Contains(l.Content, "updated task") {
			update = l
			break
		}
	}
	assert.Assert(t, update != nil, "expected an update log row")
	assert.Assert(t, strings.HasSuffix(update.Content, "updated task: name"),
		"expected log to read '... updated task: name', got %q", update.Content)

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.ChannelMessage, "")
}

func TestUpdateTask_AddInstructionsToPending_Silent(t *testing.T) {
	t.Parallel()
	// Adding instructions to a task that already has queued runner work,
	// without setting Start, must not re-announce "queued" on the channel —
	// the gate is "this call queued it" (Started), not "the task is queued"
	// (Runner). Log row still fires.
	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	srv := New(Options{Store: teststore.New(t), Publisher: pub})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{Name: "t", Runner: "r", Workspace: "w"})
	assert.NilError(t, err)
	// Task is Pending with Command=Start (PendingRunner() != ""). Don't
	// transition; the next UpdateTask runs against this queued state.
	pub.ResetCalls()

	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:              createResp.Task.Id,
		AddInstructions: []*xagentv1.Instruction{{Text: "do another thing"}},
	})
	assert.NilError(t, err)

	logs, err := srv.store.ListLogsByTask(ctx, nil, createResp.Task.Id, org.OrgID)
	assert.NilError(t, err)
	var update *model.Log
	for _, l := range logs {
		if strings.Contains(l.Content, "updated task") {
			update = l
			break
		}
	}
	assert.Assert(t, update != nil, "expected an update log row")

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.ChannelMessage, "",
		"adding instructions to a queued task must not re-announce 'queued' (only req.Start does)")
}

func TestUnarchiveTask_ClearsArchiveAfter(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:         "Auto-archive task",
		Runner:       "test-runner",
		Workspace:    "test-workspace",
		ArchiveAfter: durationpb.New(time.Hour),
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

	// Unarchive should clear archive_after so the archiver doesn't immediately re-archive.
	_, err = srv.UnarchiveTask(ctx, &xagentv1.UnarchiveTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)

	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)
	assert.Assert(t, !getResp.Task.Archived, "task should be unarchived")
	assert.Equal(t, getResp.Task.ArchiveAfter.AsDuration(), time.Duration(0), "ArchiveAfter should reset to 0 (never) on unarchive")
}
