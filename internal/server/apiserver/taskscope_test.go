package apiserver

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/agentauth"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/auth/authscope"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// This file migrates the behavioral spec that lived in
// internal/agentmcp/filter_test.go (own task / direct children / archived
// gating) onto the apiserver handlers, which now enforce per-task scopes
// directly (proposals/draft/eliminate-runner-socket-proxy.md §3/§5, with the
// two-tier AllowOp+Allow override). It also adds, for every converted handler, a
// wrong-instance test: a caller scoped to task A must be denied on task B. That
// is the safety net for the pre-gate design — the empty-scopes completeness test
// (scope_test.go) can't catch a forgotten post-load Allow because the pre-gate
// already denies empty scopes, but a wrong-instance caller passes the pre-gate
// and must be stopped by the post-load Allow.

// scopedCtx returns a context carrying a caller in org with exactly the given
// scopes — the narrow-token analogue of createCtx (which grants admin).
func scopedCtx(t *testing.T, org *teststore.Org, scopes authscope.Scopes) context.Context {
	t.Helper()
	return apiauth.WithUser(t.Context(), &apiauth.UserInfo{
		ID:     org.UserID,
		OrgID:  org.OrgID,
		Scopes: scopes,
	})
}

// taskScopes mints the scopes a runner would grant a task token for taskID,
// using the production minter so the tests track real grants.
func taskScopes(taskID int64, caps ...string) authscope.Scopes {
	return agentauth.Scopes(agentauth.ScopeOptions{
		TaskID:       taskID,
		Workspace:    "test-workspace",
		Runner:       "test-runner",
		Capabilities: caps,
	})
}

// makeArchived puts the task in a terminal state and archives it via the admin
// context, mirroring how a finished task is archived in production.
func makeArchived(t *testing.T, srv *Server, adminCtx context.Context, org *teststore.Org, id int64) {
	t.Helper()
	task, err := srv.store.GetTask(adminCtx, nil, id, org.OrgID)
	assert.NilError(t, err)
	task.Status = model.TaskStatusCompleted
	task.Command = model.TaskCommandNone
	assert.NilError(t, srv.store.UpdateTask(adminCtx, nil, task))
	_, err = srv.ArchiveTask(adminCtx, &xagentv1.ArchiveTaskRequest{Id: id})
	assert.NilError(t, err)
}

// newOrgWithTasks builds an org with the standard workspace and returns the
// admin context plus a parent task and a direct child of it.
func newOrgWithTasks(t *testing.T, srv *Server) (context.Context, *teststore.Org, *xagentv1.Task, *xagentv1.Task) {
	t.Helper()
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{
		Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}},
	})
	adminCtx := createCtx(t, org)
	parent, err := srv.CreateTask(adminCtx, &xagentv1.CreateTaskRequest{
		Name: "Parent", Runner: "test-runner", Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	child, err := srv.CreateTask(adminCtx, &xagentv1.CreateTaskRequest{
		Name: "Child", Runner: "test-runner", Workspace: "test-workspace", Parent: parent.Task.Id,
	})
	assert.NilError(t, err)
	return adminCtx, org, parent.Task, child.Task
}

// --- Behavioral spec: own task allowed, child gated on the capability ---

func TestTaskScope_GetTask_OwnAllowed_ChildGated(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	_, org, parent, child := newOrgWithTasks(t, srv)

	own := scopedCtx(t, org, taskScopes(parent.Id))
	withChild := scopedCtx(t, org, taskScopes(parent.Id, agentauth.CapabilityChildTasks))

	// Own task is always readable.
	_, err := srv.GetTask(own, &xagentv1.GetTaskRequest{Id: parent.Id})
	assert.NilError(t, err)

	// A direct child is denied without the child-tasks capability...
	_, err = srv.GetTask(own, &xagentv1.GetTaskRequest{Id: child.Id})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)

	// ...and allowed with it.
	_, err = srv.GetTask(withChild, &xagentv1.GetTaskRequest{Id: child.Id})
	assert.NilError(t, err)
}

func TestTaskScope_GetTaskDetails_OwnAllowed_ChildGated(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	_, org, parent, child := newOrgWithTasks(t, srv)

	own := scopedCtx(t, org, taskScopes(parent.Id))
	withChild := scopedCtx(t, org, taskScopes(parent.Id, agentauth.CapabilityChildTasks))

	_, err := srv.GetTaskDetails(own, &xagentv1.GetTaskDetailsRequest{Id: parent.Id})
	assert.NilError(t, err)
	_, err = srv.GetTaskDetails(own, &xagentv1.GetTaskDetailsRequest{Id: child.Id})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
	_, err = srv.GetTaskDetails(withChild, &xagentv1.GetTaskDetailsRequest{Id: child.Id})
	assert.NilError(t, err)
}

func TestTaskScope_UpdateTask_OwnAllowed_ChildGated(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	_, org, parent, child := newOrgWithTasks(t, srv)

	own := scopedCtx(t, org, taskScopes(parent.Id))
	withChild := scopedCtx(t, org, taskScopes(parent.Id, agentauth.CapabilityChildTasks))

	_, err := srv.UpdateTask(own, &xagentv1.UpdateTaskRequest{Id: parent.Id, Name: "renamed"})
	assert.NilError(t, err)
	_, err = srv.UpdateTask(own, &xagentv1.UpdateTaskRequest{Id: child.Id, Name: "hijack"})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
	_, err = srv.UpdateTask(withChild, &xagentv1.UpdateTaskRequest{Id: child.Id, Name: "ok"})
	assert.NilError(t, err)
}

func TestTaskScope_ListLogs_OwnAllowed_ChildGated(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	_, org, parent, child := newOrgWithTasks(t, srv)

	own := scopedCtx(t, org, taskScopes(parent.Id))
	withChild := scopedCtx(t, org, taskScopes(parent.Id, agentauth.CapabilityChildTasks))

	_, err := srv.ListLogs(own, &xagentv1.ListLogsRequest{TaskId: parent.Id})
	assert.NilError(t, err)
	_, err = srv.ListLogs(own, &xagentv1.ListLogsRequest{TaskId: child.Id})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
	_, err = srv.ListLogs(withChild, &xagentv1.ListLogsRequest{TaskId: child.Id})
	assert.NilError(t, err)
}

func TestTaskScope_CreateTask_ChildGated(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	_, org, parent, _ := newOrgWithTasks(t, srv)

	own := scopedCtx(t, org, taskScopes(parent.Id))
	withChild := scopedCtx(t, org, taskScopes(parent.Id, agentauth.CapabilityChildTasks))

	// Without the child-tasks capability there is no create scope at all.
	_, err := srv.CreateTask(own, &xagentv1.CreateTaskRequest{
		Parent: parent.Id, Runner: "test-runner", Workspace: "test-workspace",
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)

	// With it, a child of the own task in the same workspace/runner is allowed.
	_, err = srv.CreateTask(withChild, &xagentv1.CreateTaskRequest{
		Parent: parent.Id, Runner: "test-runner", Workspace: "test-workspace",
	})
	assert.NilError(t, err)
}

func TestTaskScope_ListChildTasks_ChildGated(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	_, org, parent, _ := newOrgWithTasks(t, srv)

	own := scopedCtx(t, org, taskScopes(parent.Id))
	withChild := scopedCtx(t, org, taskScopes(parent.Id, agentauth.CapabilityChildTasks))

	// Listing children needs the child read scope (task.read:{task.parent:self}).
	_, err := srv.ListChildTasks(own, &xagentv1.ListChildTasksRequest{ParentId: parent.Id})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)

	resp, err := srv.ListChildTasks(withChild, &xagentv1.ListChildTasksRequest{ParentId: parent.Id})
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Tasks), 1)
}

// --- Behavioral spec: archived gating ---
//
// In this slice the minter does not yet emit task.archived:"false" (that is
// Phase 2). These tests build the archived-constrained scope directly to prove
// the handlers pass the task's real archived state and the predicate denies an
// archived task uniformly for reads and writes.

func TestTaskScope_ArchivedGating(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	adminCtx, org, active, archived := newOrgWithTasks(t, srv)
	makeArchived(t, srv, adminCtx, org, archived.Id)

	readScope := func(id int64) authscope.Scopes {
		return authscope.Scopes{authscope.New(authscope.OpTaskRead,
			authscope.WithTaskID(id), authscope.WithTaskArchived(false))}
	}
	writeScope := func(id int64) authscope.Scopes {
		return authscope.Scopes{authscope.New(authscope.OpTaskWrite,
			authscope.WithTaskID(id), authscope.WithTaskArchived(false))}
	}

	// An active task carries task.archived:"false" → matches → allowed.
	_, err := srv.GetTask(scopedCtx(t, org, readScope(active.Id)), &xagentv1.GetTaskRequest{Id: active.Id})
	assert.NilError(t, err)

	// An archived task carries task.archived:"true" → fails the "false"
	// predicate → denied, for reads...
	_, err = srv.GetTask(scopedCtx(t, org, readScope(archived.Id)), &xagentv1.GetTaskRequest{Id: archived.Id})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)

	// ...and writes (this is the unarchive-resurrect hole, closed for free).
	_, err = srv.UnarchiveTask(scopedCtx(t, org, writeScope(archived.Id)), &xagentv1.UnarchiveTaskRequest{Id: archived.Id})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

// --- Wrong-instance: caller scoped to task A is denied on task B ---

// taskInstanceHandlers are the row-loading task handlers whose post-load Allow
// must reject a caller scoped to a different task. Each closure invokes the
// handler against the given task id with the caller in ctx.
func taskInstanceHandlers() []struct {
	name string
	call func(ctx context.Context, srv *Server, id int64) error
} {
	return []struct {
		name string
		call func(ctx context.Context, srv *Server, id int64) error
	}{
		{"GetTask", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: id})
			return err
		}},
		{"GetTaskDetails", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: id})
			return err
		}},
		{"UpdateTask", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: id, Name: "x"})
			return err
		}},
		{"ArchiveTask", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.ArchiveTask(ctx, &xagentv1.ArchiveTaskRequest{Id: id})
			return err
		}},
		{"UnarchiveTask", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.UnarchiveTask(ctx, &xagentv1.UnarchiveTaskRequest{Id: id})
			return err
		}},
		{"CancelTask", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.CancelTask(ctx, &xagentv1.CancelTaskRequest{Id: id})
			return err
		}},
		{"RestartTask", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.RestartTask(ctx, &xagentv1.RestartTaskRequest{Id: id})
			return err
		}},
		{"CreateLink", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{TaskId: id, Url: "https://example.com/x"})
			return err
		}},
		{"ListLinks", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.ListLinks(ctx, &xagentv1.ListLinksRequest{TaskId: id})
			return err
		}},
		{"UploadLogs", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.UploadLogs(ctx, &xagentv1.UploadLogsRequest{
				TaskId: id, Entries: []*xagentv1.LogEntry{{Type: "info", Content: "x"}},
			})
			return err
		}},
		{"ListLogs", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.ListLogs(ctx, &xagentv1.ListLogsRequest{TaskId: id})
			return err
		}},
		{"ListEventsByTask", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.ListEventsByTask(ctx, &xagentv1.ListEventsByTaskRequest{TaskId: id})
			return err
		}},
		{"SubmitRunnerEvents", func(ctx context.Context, srv *Server, id int64) error {
			_, err := srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
				Events: []*xagentv1.RunnerEvent{{TaskId: id, Event: "started", Version: 1}},
			})
			return err
		}},
	}
}

func TestTaskScope_WrongInstanceDenied(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	_, org, taskA, _ := newOrgWithTasks(t, srv)
	// taskB is an unrelated task in the same org.
	taskB, err := srv.CreateTask(createCtx(t, org), &xagentv1.CreateTaskRequest{
		Name: "B", Runner: "test-runner", Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Scoped to taskA only (read+write on its id), no child capability.
	ctxA := scopedCtx(t, org, taskScopes(taskA.Id))

	for _, h := range taskInstanceHandlers() {
		t.Run(h.name, func(t *testing.T) {
			err := h.call(ctxA, srv, taskB.Task.Id)
			assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied,
				"%s scoped to task A must deny task B", h.name)
		})
	}
}

func TestTaskScope_CreateTask_WrongParentDenied(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	_, org, taskA, _ := newOrgWithTasks(t, srv)
	taskB, err := srv.CreateTask(createCtx(t, org), &xagentv1.CreateTaskRequest{
		Name: "B", Runner: "test-runner", Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Child-tasks capability scoped to taskA: may create children of A, not of B.
	ctxA := scopedCtx(t, org, taskScopes(taskA.Id, agentauth.CapabilityChildTasks))
	_, err = srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
		Parent: taskB.Task.Id, Runner: "test-runner", Workspace: "test-workspace",
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestTaskScope_ListChildTasks_WrongParentDenied(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	_, org, taskA, _ := newOrgWithTasks(t, srv)
	taskB, err := srv.CreateTask(createCtx(t, org), &xagentv1.CreateTaskRequest{
		Name: "B", Runner: "test-runner", Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	ctxA := scopedCtx(t, org, taskScopes(taskA.Id, agentauth.CapabilityChildTasks))
	_, err = srv.ListChildTasks(ctxA, &xagentv1.ListChildTasksRequest{ParentId: taskB.Task.Id})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

// AddEventTask / RemoveEventTask are dual-gated (event.write + task.write). A
// caller holding event.write but a task scope for A only must still be denied on
// task B's task half.
func TestTaskScope_EventTask_WrongInstanceDenied(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	adminCtx, org, taskA, _ := newOrgWithTasks(t, srv)
	taskB, err := srv.CreateTask(adminCtx, &xagentv1.CreateTaskRequest{
		Name: "B", Runner: "test-runner", Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	ev, err := srv.CreateEvent(adminCtx, &xagentv1.CreateEventRequest{Description: "e", Url: "https://example.com/e"})
	assert.NilError(t, err)

	// event.write (coarse) plus a task scope bound to A only.
	scopes := authscope.Scopes{
		authscope.New(authscope.OpEventWrite),
		authscope.New(authscope.OpTaskWrite, authscope.WithTaskID(taskA.Id)),
	}
	ctxA := scopedCtx(t, org, scopes)

	_, err = srv.AddEventTask(ctxA, &xagentv1.AddEventTaskRequest{EventId: ev.Event.Id, TaskId: taskB.Task.Id})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)

	_, err = srv.RemoveEventTask(ctxA, &xagentv1.RemoveEventTaskRequest{EventId: ev.Event.Id, TaskId: taskB.Task.Id})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

// SubmitRunnerEvents authorizes per-event; a partial-batch failure is accepted
// (the own-task event is applied before the foreign event is rejected).
func TestTaskScope_SubmitRunnerEvents_PartialBatch(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	adminCtx, org, taskA, _ := newOrgWithTasks(t, srv)
	taskB, err := srv.CreateTask(adminCtx, &xagentv1.CreateTaskRequest{
		Name: "B", Runner: "test-runner", Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	ctxA := scopedCtx(t, org, taskScopes(taskA.Id))
	_, err = srv.SubmitRunnerEvents(ctxA, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: taskA.Id, Event: "started", Version: 1},
			{TaskId: taskB.Task.Id, Event: "started", Version: 1},
		},
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)

	// The own-task event was applied before the foreign one was rejected.
	got, err := srv.GetTask(adminCtx, &xagentv1.GetTaskRequest{Id: taskA.Id})
	assert.NilError(t, err)
	assert.Equal(t, got.Task.Status, xagentv1.TaskStatus_RUNNING)
}
