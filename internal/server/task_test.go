package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"
)

func TestGetTask(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

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
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	createResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, errA := srv.GetTask(userA, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	_, errB := srv.GetTask(userB, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})

	// Assert
	assert.NilError(t, errA)
	assert.ErrorContains(t, errB, "not found")
}

func TestGetTaskDetails_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	createResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, errA := srv.GetTaskDetails(userA, &xagentv1.GetTaskDetailsRequest{Id: createResp.Task.Id})
	_, errB := srv.GetTaskDetails(userB, &xagentv1.GetTaskDetailsRequest{Id: createResp.Task.Id})

	// Assert
	assert.NilError(t, errA)
	assert.ErrorContains(t, errB, "not found")
}

func TestCreateTask(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

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
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	parentResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Parent Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.CreateTask(userB, &xagentv1.CreateTaskRequest{
		Name:      "User B's Child Task",
		Workspace: "test-workspace",
		Parent:    parentResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestListTasks(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)
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

func TestListTasks_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	_, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task 1",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task 2",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(userB, &xagentv1.CreateTaskRequest{
		Name:      "User B's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	respA, err := srv.ListTasks(userA, &xagentv1.ListTasksRequest{})
	assert.NilError(t, err)
	respB, err := srv.ListTasks(userB, &xagentv1.ListTasksRequest{})
	assert.NilError(t, err)

	// Assert
	assert.Equal(t, len(respA.Tasks), 2)
	assert.Equal(t, len(respB.Tasks), 1)
}

func TestListChildTasks_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	parentResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Parent Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Child Task",
		Workspace: "test-workspace",
		Parent:    parentResp.Task.Id,
	})
	assert.NilError(t, err)

	// Act
	respA, err := srv.ListChildTasks(userA, &xagentv1.ListChildTasksRequest{
		ParentId: parentResp.Task.Id,
	})
	assert.NilError(t, err)
	respB, err := srv.ListChildTasks(userB, &xagentv1.ListChildTasksRequest{
		ParentId: parentResp.Task.Id,
	})
	assert.NilError(t, err)

	// Assert
	assert.Equal(t, len(respA.Tasks), 1)
	assert.Equal(t, len(respB.Tasks), 0)
}

func TestCreateTask_ValidRunnerAndWorkspace(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

	// Register workspaces for runner
	_, err := srv.RegisterWorkspaces(ctx, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "my-workspace"},
		},
	})
	assert.NilError(t, err)

	// Create task with valid runner and workspace
	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Valid Task",
		Runner:    "runner-1",
		Workspace: "my-workspace",
	})
	assert.NilError(t, err)
	assert.Equal(t, resp.Task.Runner, "runner-1")
	assert.Equal(t, resp.Task.Workspace, "my-workspace")
}

func TestCreateTask_NonExistentRunner(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

	// Create task with non-existent runner
	_, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Bad Task",
		Runner:    "nonexistent-runner",
		Workspace: "my-workspace",
	})
	assert.ErrorContains(t, err, "runner \"nonexistent-runner\" not found")
}

func TestCreateTask_NonExistentWorkspace(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

	// Register workspaces for runner
	_, err := srv.RegisterWorkspaces(ctx, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "real-workspace"},
		},
	})
	assert.NilError(t, err)

	// Create task with valid runner but non-existent workspace
	_, err = srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Bad Task",
		Runner:    "runner-1",
		Workspace: "fake-workspace",
	})
	assert.ErrorContains(t, err, "workspace \"fake-workspace\" not found on runner \"runner-1\"")
}

func TestCreateTask_NoRunner(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

	// Create task without runner (should still work for backwards compatibility)
	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "No Runner Task",
		Workspace: "any-workspace",
	})
	assert.NilError(t, err)
	assert.Equal(t, resp.Task.Runner, "")
	assert.Equal(t, resp.Task.Workspace, "any-workspace")
}

func TestCreateTask_RunnerOnlyNoWorkspace(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

	// Register workspaces for runner
	_, err := srv.RegisterWorkspaces(ctx, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "my-workspace"},
		},
	})
	assert.NilError(t, err)

	// Create task with valid runner but no workspace specified
	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:   "Runner Only Task",
		Runner: "runner-1",
	})
	assert.NilError(t, err)
	assert.Equal(t, resp.Task.Runner, "runner-1")
}

func TestUpdateTask(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)
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

func TestUpdateTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	createResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.UpdateTask(userB, &xagentv1.UpdateTaskRequest{
		Id:   createResp.Task.Id,
		Name: "Hijacked Name",
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestArchiveTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	createResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.ArchiveTask(userB, &xagentv1.ArchiveTaskRequest{
		Id: createResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestCancelTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	createResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.CancelTask(userB, &xagentv1.CancelTaskRequest{
		Id: createResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestRestartTask_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	createResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.RestartTask(userB, &xagentv1.RestartTaskRequest{
		Id: createResp.Task.Id,
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}
