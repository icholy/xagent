package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"
)

func TestGetTask(t *testing.T) {
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")

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
		Owner:     "test-user",
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
		Command:   "start",
		Version:   1,
		CreatedAt: getResp.Task.CreatedAt, // Copy timestamps since we can't predict them
		UpdatedAt: getResp.Task.UpdatedAt,
	}

	assert.DeepEqual(t, getResp.Task, expected, protocmp.Transform())
}

func TestGetTask_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
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
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
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
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")

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
		Owner:     "test-user",
		Instructions: []*xagentv1.Instruction{
			{
				Text: "Do something",
				Url:  "https://example.com/issue/1",
			},
		},
		Status:    "pending",
		Command:   "start",
		Version:   1,
		CreatedAt: resp.Task.CreatedAt,
		UpdatedAt: resp.Task.UpdatedAt,
	}
	assert.DeepEqual(t, resp.Task, expected, protocmp.Transform())
}

func TestCreateTask_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
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
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")
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
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
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
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
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

func TestUpdateTask(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")
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
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
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

func TestDeleteTask(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")
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

func TestDeleteTask_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
	createResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act - delete is a no-op for tasks you don't own
	_, err = srv.DeleteTask(userB, &xagentv1.DeleteTaskRequest{
		Id: createResp.Task.Id,
	})
	assert.NilError(t, err)

	// Assert - task still exists for user A
	_, err = srv.GetTask(userA, &xagentv1.GetTaskRequest{Id: createResp.Task.Id})
	assert.NilError(t, err)
}

func TestArchiveTask_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
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
