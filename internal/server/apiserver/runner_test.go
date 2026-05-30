package apiserver

import (
	"context"
	"fmt"
	"testing"

	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestSubmitRunnerEvents(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	// Create a task (starts as pending with start command)
	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Test Task",
		Runner:    "test-runner",
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
	_, err = srv.SubmitRunnerEvents(ctxB, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: taskResp.Task.Id, Event: "started", Version: 1},
		},
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestListRunnerTasks(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{
		Workspaces: []teststore.WorkspaceOptions{
			{RunnerID: "runner-1", Name: "test-workspace"},
			{RunnerID: "runner-2", Name: "test-workspace"},
		},
	})
	ctx := createCtx(t, org)
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
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{
		Workspaces: []teststore.WorkspaceOptions{
			{RunnerID: "runner-1", Name: "test-workspace"},
		},
	})
	ctx := createCtx(t, org)
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

func TestSubmitRunnerEvents_RichLogIncludesResultingStatus(t *testing.T) {
	t.Parallel()
	// A re-queue case: an "stopped" event with a pending Start command sends
	// the task back to Pending. The rich log must distinguish that from a
	// clean Completed exit; the notification stays silent (the eventual
	// terminal exit speaks for itself).
	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	srv := New(Options{Store: teststore.New(t), Publisher: pub})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{Name: "t", Runner: "r", Workspace: "w"})
	assert.NilError(t, err)
	taskID := createResp.Task.Id

	// Start then re-queue: started → running (Command cleared) → UpdateTask
	// with Start re-arms the Start command → stopped re-queues the task.
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: taskID, Event: "started", Version: 1}},
	})
	assert.NilError(t, err)
	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: taskID, Start: true})
	assert.NilError(t, err)
	pub.ResetCalls()

	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: taskID, Event: "stopped", Version: 0}},
	})
	assert.NilError(t, err)

	// Re-queue log row must say "task pending" (not "exited successfully").
	logs, err := srv.store.ListLogsByTask(ctx, nil, taskID, org.OrgID)
	assert.NilError(t, err)
	var requeueLog *model.Log
	for _, l := range logs {
		if l.Content == "container exited; task pending" {
			requeueLog = l
			break
		}
	}
	assert.Assert(t, requeueLog != nil, "expected a re-queue log row, got logs: %v", logContents(logs))

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.ChannelMessage, "", "re-queue must not emit a channel message")
}

func TestSubmitRunnerEvents_TerminalExitFiresChannelMessage(t *testing.T) {
	t.Parallel()
	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	srv := New(Options{Store: teststore.New(t), Publisher: pub})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "r", Name: "w"}}})
	ctx := createCtx(t, org)

	createResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{Name: "t", Runner: "r", Workspace: "w"})
	assert.NilError(t, err)
	taskID := createResp.Task.Id

	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: taskID, Event: "started", Version: 1}},
	})
	assert.NilError(t, err)
	pub.ResetCalls()

	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: taskID, Event: "stopped", Version: 0}},
	})
	assert.NilError(t, err)

	logs, err := srv.store.ListLogsByTask(ctx, nil, taskID, org.OrgID)
	assert.NilError(t, err)
	var completedLog *model.Log
	for _, l := range logs {
		if l.Content == "container exited; task completed" {
			completedLog = l
			break
		}
	}
	assert.Assert(t, completedLog != nil, "expected a Completed log row, got logs: %v", logContents(logs))

	calls := pub.PublishCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].N.ChannelMessage, fmt.Sprintf("Task %d completed.", taskID))
}

func logContents(logs []*model.Log) []string {
	out := make([]string, len(logs))
	for i, l := range logs {
		out[i] = l.Content
	}
	return out
}

func TestListRunnerTasks_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	runnerWorkspaces := &teststore.OrgOptions{
		Workspaces: []teststore.WorkspaceOptions{
			{RunnerID: "runner-1", Name: "test-workspace"},
		},
	}
	orgA := teststore.CreateOrg(t, srv.store, runnerWorkspaces)
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, runnerWorkspaces)
	ctxB := createCtx(t, orgB)
	_, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
		Runner:    "runner-1",
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(ctxB, &xagentv1.CreateTaskRequest{
		Name:      "User B's Task",
		Workspace: "test-workspace",
		Runner:    "runner-1",
	})
	assert.NilError(t, err)

	// Act
	respA, err := srv.ListRunnerTasks(ctxA, &xagentv1.ListRunnerTasksRequest{
		Runner: "runner-1",
	})
	assert.NilError(t, err)
	respB, err := srv.ListRunnerTasks(ctxB, &xagentv1.ListRunnerTasksRequest{
		Runner: "runner-1",
	})
	assert.NilError(t, err)

	// Assert
	assert.Equal(t, len(respA.Tasks), 1)
	assert.Equal(t, respA.Tasks[0].Name, "User A's Task")
	assert.Equal(t, len(respB.Tasks), 1)
	assert.Equal(t, respB.Tasks[0].Name, "User B's Task")
}
