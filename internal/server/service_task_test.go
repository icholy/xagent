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

func TestGetTaskPermissions(t *testing.T) {
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
