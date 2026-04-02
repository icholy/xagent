package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestGetTask(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	ctx, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})

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
		Status:    xagentv1.TaskStatus_PENDING,
		Command:   xagentv1.TaskCommand_START,
		Actions:   &xagentv1.TaskActions{Cancel: true},
		Version:   1,
		CreatedAt: getResp.Task.CreatedAt, // Copy timestamps since we can't predict them
		UpdatedAt: getResp.Task.UpdatedAt,
	}

	assert.DeepEqual(t, getResp.Task, expected, protocmp.Transform())
}

func TestGetTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	ctxA, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
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
	ctxA, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
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
	ctx, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})

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
		Status:    xagentv1.TaskStatus_PENDING,
		Command:   xagentv1.TaskCommand_START,
		Actions:   &xagentv1.TaskActions{Cancel: true},
		Version:   1,
		CreatedAt: resp.Task.CreatedAt,
		UpdatedAt: resp.Task.UpdatedAt,
	}
	assert.DeepEqual(t, resp.Task, expected, protocmp.Transform())
}

func TestCreateTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	ctxA, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
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
	ctx, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
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
	ctxA, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
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
	ctxA, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
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
	ctx, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})

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
	ctx, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})

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
	ctx, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
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
	ctxA, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
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
	ctxA, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
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
	ctxA, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
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
	ctxA, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxB, _ := createTestOrg(t, srv, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
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
