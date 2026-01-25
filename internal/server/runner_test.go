package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"gotest.tools/v3/assert"
)

func TestSubmitRunnerEvents(t *testing.T) {
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")

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
	assert.Equal(t, getResp.Task.Status, "pending")
	assert.Equal(t, getResp.Task.Command, "start")
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
	assert.Equal(t, getResp.Task.Status, "running")
	assert.Equal(t, getResp.Task.Command, "")

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
	assert.Equal(t, getResp.Task.Status, "completed")
}
