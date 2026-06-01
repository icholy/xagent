package apiserver

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestCreateLink(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with Link",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    taskResp.Task.Id,
		Relevance: "Related PR",
		Url:       "https://github.com/example/repo/pull/123",
		Title:     "Fix bug",
		Subscribe: true,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, resp.Link.TaskId, taskResp.Task.Id)
	assert.Equal(t, resp.Link.Url, "https://github.com/example/repo/pull/123")
	assert.Equal(t, resp.Link.Subscribe, true)
}

func TestCreateLink_DerivesRoutingKey(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with Link",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act - no routing_key supplied, so the server derives it from url
	resp, err := srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId: taskResp.Task.Id,
		Url:    "https://github.com/example/repo/pull/123#issuecomment-9",
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, resp.Link.Url, "https://github.com/example/repo/pull/123#issuecomment-9")
	assert.Equal(t, resp.Link.RoutingKey, "https://github.com/example/repo/pull/123")
}

func TestCreateLink_RoutingKeyOverride(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with Link",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act - explicit routing_key is used verbatim, not derived from url
	resp, err := srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:     taskResp.Task.Id,
		Url:        "https://github.com/example/repo/pull/123#issuecomment-9",
		RoutingKey: "https://example.com/custom",
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, resp.Link.RoutingKey, "https://example.com/custom")
}

func TestCreateLink_Permissions(t *testing.T) {
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
	_, err = srv.CreateLink(ctxB, &xagentv1.CreateLinkRequest{
		TaskId:    taskResp.Task.Id,
		Relevance: "Sneaky link",
		Url:       "https://github.com/example/repo/pull/123",
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestListLinks(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, &teststore.OrgOptions{Workspaces: []teststore.WorkspaceOptions{{RunnerID: "test-runner", Name: "test-workspace"}}})
	ctx := createCtx(t, org)
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with Links",
		Runner:    "test-runner",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    taskResp.Task.Id,
		Relevance: "Link 1",
		Url:       "https://github.com/example/repo/pull/1",
	})
	assert.NilError(t, err)
	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    taskResp.Task.Id,
		Relevance: "Link 2",
		Url:       "https://github.com/example/repo/pull/2",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListLinks(ctx, &xagentv1.ListLinksRequest{
		TaskId: taskResp.Task.Id,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Links), 2)
}

func TestListLinks_Permissions(t *testing.T) {
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
	_, err = srv.CreateLink(ctxA, &xagentv1.CreateLinkRequest{
		TaskId:    taskResp.Task.Id,
		Relevance: "User A's Link",
		Url:       "https://github.com/example/repo/pull/1",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListLinks(ctxB, &xagentv1.ListLinksRequest{
		TaskId: taskResp.Task.Id,
	})

	// Assert - User B gets empty list, not an error
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Links), 0)
}
