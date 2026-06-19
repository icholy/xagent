package apiserver

import (
	"context"
	"slices"
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// lifecycleEvents returns the task's lifecycle events most-recent-first.
// ListEventsByTask returns chronological (oldest-first) order, so the collected
// events are reversed to put the most recent lifecycle event at index 0.
func lifecycleEvents(t *testing.T, srv *Server, ctx context.Context, taskID int64) []*xagentv1.LifecyclePayload {
	t.Helper()
	resp, err := srv.ListEventsByTask(ctx, &xagentv1.ListEventsByTaskRequest{TaskId: taskID})
	assert.NilError(t, err)
	var out []*xagentv1.LifecyclePayload
	for _, e := range resp.Events {
		if l := e.GetLifecycle(); l != nil {
			out = append(out, l)
		}
	}
	slices.Reverse(out)
	return out
}

func lifecycleTestServer(t *testing.T) (*Server, context.Context) {
	t.Helper()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	return srv, createCtx(t, org)
}

func TestLifecycle_CreateAppendsCreatedEvent(t *testing.T) {
	t.Parallel()
	srv, ctx := lifecycleTestServer(t)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	events := lifecycleEvents(t, srv, ctx, resp.Task.Id)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].Kind, xagentv1.LifecycleKind_LIFECYCLE_KIND_CREATED)
	assert.Equal(t, events[0].Actor.Kind, "user")
	// A freshly created task has no prior status; it lands in PENDING.
	assert.Equal(t, events[0].FromStatus, "")
	assert.Equal(t, events[0].ToStatus, "Pending")
}

func TestLifecycle_CancelAppendsCancelledEventBesideProjection(t *testing.T) {
	t.Parallel()
	srv, ctx := lifecycleTestServer(t)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	taskID := resp.Task.Id

	_, err = srv.CancelTask(ctx, &xagentv1.CancelTaskRequest{Id: taskID})
	assert.NilError(t, err)

	// The lifecycle event records the transition...
	events := lifecycleEvents(t, srv, ctx, taskID)
	assert.Equal(t, events[0].Kind, xagentv1.LifecycleKind_LIFECYCLE_KIND_CANCELLED)
	assert.Equal(t, events[0].FromStatus, "Pending")
	assert.Equal(t, events[0].ToStatus, "Cancelled")
	// ...and the materialized status projection is updated in the same tx.
	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Task.Status, xagentv1.TaskStatus_CANCELLED)
}

func TestLifecycle_TaskMutationsAppendEvents(t *testing.T) {
	t.Parallel()
	srv, ctx := lifecycleTestServer(t)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	taskID := resp.Task.Id

	// A pending task can't be restarted, so start its sandbox first, then restart
	// the running task. Runner events use version 0 to bypass the version check.
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: taskID, Event: "started", Version: 0}},
	})
	assert.NilError(t, err)
	_, err = srv.RestartTask(ctx, &xagentv1.RestartTaskRequest{Id: taskID})
	assert.NilError(t, err)

	// Drive it back to running, then to a terminal status so it can be archived.
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: taskID, Event: "started", Version: 0}},
	})
	assert.NilError(t, err)
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: taskID, Event: "stopped", Version: 0}},
	})
	assert.NilError(t, err)

	_, err = srv.ArchiveTask(ctx, &xagentv1.ArchiveTaskRequest{Id: taskID})
	assert.NilError(t, err)
	_, err = srv.UnarchiveTask(ctx, &xagentv1.UnarchiveTaskRequest{Id: taskID})
	assert.NilError(t, err)

	// Collect the set of lifecycle kinds present on the stream.
	kinds := map[xagentv1.LifecycleKind]bool{}
	for _, e := range lifecycleEvents(t, srv, ctx, taskID) {
		kinds[e.Kind] = true
	}
	assert.Assert(t, kinds[xagentv1.LifecycleKind_LIFECYCLE_KIND_CREATED])
	assert.Assert(t, kinds[xagentv1.LifecycleKind_LIFECYCLE_KIND_RESTARTED])
	assert.Assert(t, kinds[xagentv1.LifecycleKind_LIFECYCLE_KIND_SANDBOX_STARTED])
	assert.Assert(t, kinds[xagentv1.LifecycleKind_LIFECYCLE_KIND_SANDBOX_EXITED])
	assert.Assert(t, kinds[xagentv1.LifecycleKind_LIFECYCLE_KIND_ARCHIVED])
	assert.Assert(t, kinds[xagentv1.LifecycleKind_LIFECYCLE_KIND_UNARCHIVED])
}

func TestLifecycle_AddInstructionsSkipsRedundantUpdatedEvent(t *testing.T) {
	t.Parallel()
	srv, ctx := lifecycleTestServer(t)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	taskID := resp.Task.Id

	// Adding an instruction appends an instruction event of its own, so it must
	// not also emit a redundant Updated lifecycle event (issue #1030). Only the
	// Created event from CreateTask should remain.
	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:              taskID,
		AddInstructions: []*xagentv1.Instruction{{Text: "do the thing"}},
	})
	assert.NilError(t, err)

	events := lifecycleEvents(t, srv, ctx, taskID)
	assert.Equal(t, len(events), 1)
	assert.Equal(t, events[0].Kind, xagentv1.LifecycleKind_LIFECYCLE_KIND_CREATED)

	// A non-instruction field change still records an Updated event, and the
	// instructions added alongside it are not listed on it.
	_, err = srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
		Id:              taskID,
		Name:            "Renamed",
		AddInstructions: []*xagentv1.Instruction{{Text: "and another"}},
	})
	assert.NilError(t, err)

	events = lifecycleEvents(t, srv, ctx, taskID)
	assert.Equal(t, len(events), 2)
	assert.Equal(t, events[0].Kind, xagentv1.LifecycleKind_LIFECYCLE_KIND_UPDATED)
	assert.DeepEqual(t, events[0].Fields, []string{"name"})
}

func TestLifecycle_RunnerEventsAppendSandboxEvents(t *testing.T) {
	t.Parallel()
	srv, ctx := lifecycleTestServer(t)

	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	taskID := resp.Task.Id

	// started: PENDING -> RUNNING, actor is the runner.
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: taskID, Event: "started", Version: 1}},
	})
	assert.NilError(t, err)

	events := lifecycleEvents(t, srv, ctx, taskID)
	assert.Equal(t, events[0].Kind, xagentv1.LifecycleKind_LIFECYCLE_KIND_SANDBOX_STARTED)
	assert.Equal(t, events[0].Actor.Kind, "runner")
	assert.Equal(t, events[0].FromStatus, "Pending")
	assert.Equal(t, events[0].ToStatus, "Running")

	// failed: RUNNING -> FAILED, with the failure detail in message.
	_, err = srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: taskID, Event: "failed", Version: 0}},
	})
	assert.NilError(t, err)

	events = lifecycleEvents(t, srv, ctx, taskID)
	assert.Equal(t, events[0].Kind, xagentv1.LifecycleKind_LIFECYCLE_KIND_SANDBOX_FAILED)
	assert.Equal(t, events[0].Message, "container failed")
	assert.Equal(t, events[0].ToStatus, "Failed")
}
