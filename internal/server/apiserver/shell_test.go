package apiserver

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// newShellMock returns a ShellRegistry mock whose Seed always succeeds.
func newShellMock() *ShellRegistryMock {
	return &ShellRegistryMock{
		SeedFunc: func(id string, orgID int64) error { return nil },
	}
}

// createTask creates a task and returns its id.
func createTask(t *testing.T, srv *Server, ctx context.Context) int64 {
	t.Helper()
	resp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	return resp.Task.Id
}

// event drives a task through a runner lifecycle event (version 0 bypasses the
// version check).
func event(t *testing.T, srv *Server, ctx context.Context, taskID int64, e string) {
	t.Helper()
	_, err := srv.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{{TaskId: taskID, Event: e, Version: 0}},
	})
	assert.NilError(t, err)
}

// completedTask creates a task and drives it to a terminal (completed) status.
func completedTask(t *testing.T, srv *Server, ctx context.Context) int64 {
	t.Helper()
	id := createTask(t, srv, ctx)
	event(t, srv, ctx, id, "started")
	event(t, srv, ctx, id, "stopped")
	return id
}

func TestOpenShell(t *testing.T) {
	t.Parallel()
	shell := newShellMock()
	srv := New(Options{Store: teststore.New(t), Shells: shell})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)
	taskID := completedTask(t, srv, ctx)

	// Act
	resp, err := srv.OpenShell(ctx, &xagentv1.OpenShellRequest{TaskId: taskID})

	// Assert: a session id is returned...
	assert.NilError(t, err)
	assert.Assert(t, resp.SessionId != "")

	// ...it is recorded on the task's shell_session and a start is issued so the
	// runner brings the sandbox up against the preserved disk...
	getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
	assert.NilError(t, err)
	assert.Equal(t, getResp.Task.ShellSession, resp.SessionId)
	assert.Equal(t, getResp.Task.Command, xagentv1.TaskCommand_START)
	assert.Equal(t, getResp.Task.Status, xagentv1.TaskStatus_PENDING)

	// ...and the rendezvous is seeded with the caller's org.
	calls := shell.SeedCalls()
	assert.Equal(t, len(calls), 1)
	assert.Equal(t, calls[0].ID, resp.SessionId)
	assert.Equal(t, calls[0].OrgID, org.OrgID)
}

func TestOpenShell_RejectsNonTerminal(t *testing.T) {
	t.Parallel()
	shell := newShellMock()
	srv := New(Options{Store: teststore.New(t), Shells: shell})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)

	// A pending task and a running task are both non-terminal: opening a shell
	// must fail rather than displace a live (or about-to-be-live) run.
	pending := createTask(t, srv, ctx)
	running := createTask(t, srv, ctx)
	event(t, srv, ctx, running, "started")

	for _, taskID := range []int64{pending, running} {
		_, err := srv.OpenShell(ctx, &xagentv1.OpenShellRequest{TaskId: taskID})
		assert.Equal(t, connect.CodeOf(err), connect.CodeFailedPrecondition)
	}
	// Nothing was seeded, and neither task had a shell session recorded.
	assert.Equal(t, len(shell.SeedCalls()), 0)
	for _, taskID := range []int64{pending, running} {
		getResp, err := srv.GetTask(ctx, &xagentv1.GetTaskRequest{Id: taskID})
		assert.NilError(t, err)
		assert.Equal(t, getResp.Task.ShellSession, "")
	}
}

func TestOpenShell_CrossOrgDenied(t *testing.T) {
	t.Parallel()
	shell := newShellMock()
	srv := New(Options{Store: teststore.New(t), Shells: shell})
	orgA := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	orgB := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctxA := createCtx(t, orgA)
	ctxB := createCtx(t, orgB)
	taskID := completedTask(t, srv, ctxA)

	// A caller in another org cannot open a shell for orgA's task.
	_, err := srv.OpenShell(ctxB, &xagentv1.OpenShellRequest{TaskId: taskID})
	assert.Assert(t, err != nil)
	assert.Equal(t, len(shell.SeedCalls()), 0)
}
