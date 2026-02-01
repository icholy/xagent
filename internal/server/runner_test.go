package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"gotest.tools/v3/assert"
)

func TestSubmitRunnerEvents(t *testing.T) {
	srv := setupTestServer(t)
	ctx := randomUserID(t)

	// Create a task (starts as pending with start command)
	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Test Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	taskID := createResp.Task.Id

	// Verify initial state
	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Task.Status, xagentv1.TaskStatus_PENDING)
	assert.Equal(t, getResp.Task.Command, xagentv1.TaskCommand_START)
	assert.Equal(t, getResp.Task.Version, int64(1))

	// Send started event (simulating container start)
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{
				TaskId:  taskID,
				Event:   "started",
				Version: 1,
			},
		},
	})
	assert.NilError(t, err)

	// Verify task is running and command is cleared
	getResp, err = srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Task.Status, xagentv1.TaskStatus_RUNNING)
	assert.Equal(t, getResp.Task.Command, xagentv1.TaskCommand_NONE)

	// Send stopped event (simulating container exit with code 0)
	// Use version 0 to bypass version check (spontaneous event)
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

	// Verify task status was updated to completed
	getResp, err = srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Task.Status, xagentv1.TaskStatus_COMPLETED)
}

func TestSubmitRunnerEvents_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := randomUserID(t)
	userB := randomUserID(t)
	taskResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.SubmitRunnerEvents(userB, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: taskResp.Task.Id, Event: "started", Version: 1},
		},
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestListRunnerTasks(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := randomUserID(t)
	_, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task for runner-1",
		Workspace: "test-workspace",
		Runner:    "runner-1",
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task for runner-2",
		Workspace: "test-workspace",
		Runner:    "runner-2",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListRunnerTasks(ctx, &xagentv1.ListRunnerTasksRequest{
		Runner: "runner-1",
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Tasks), 1)
	assert.Equal(t, resp.Tasks[0].Name, "Task for runner-1")
}

func TestListRunnerTasks_OnlyWithCommand(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := randomUserID(t)
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with command",
		Workspace: "test-workspace",
		Runner:    "runner-1",
	})
	assert.NilError(t, err)
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: taskResp.Task.Id, Event: "started", Version: 1},
		},
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListRunnerTasks(ctx, &xagentv1.ListRunnerTasksRequest{
		Runner: "runner-1",
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Tasks), 0)
}

func TestListRunnerTasks_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := randomUserID(t)
	userB := randomUserID(t)
	_, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
		Runner:    "runner-1",
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(userB, &xagentv1.CreateTaskRequest{
		Name:      "User B's Task",
		Workspace: "test-workspace",
		Runner:    "runner-1",
	})
	assert.NilError(t, err)

	// Act
	respA, err := srv.ListRunnerTasks(userA, &xagentv1.ListRunnerTasksRequest{
		Runner: "runner-1",
	})
	assert.NilError(t, err)
	respB, err := srv.ListRunnerTasks(userB, &xagentv1.ListRunnerTasksRequest{
		Runner: "runner-1",
	})
	assert.NilError(t, err)

	// Assert
	assert.Equal(t, len(respA.Tasks), 1)
	assert.Equal(t, respA.Tasks[0].Name, "User A's Task")
	assert.Equal(t, len(respB.Tasks), 1)
	assert.Equal(t, respB.Tasks[0].Name, "User B's Task")
}
