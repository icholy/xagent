package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"gotest.tools/v3/assert"
)

func TestCreateLink(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with Link",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    taskResp.Task.Id,
		Relevance: "Related PR",
		Url:       "https://github.com/example/repo/pull/123",
		Title:     "Fix bug",
		Notify:    true,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, resp.Link.TaskId, taskResp.Task.Id)
	assert.Equal(t, resp.Link.Url, "https://github.com/example/repo/pull/123")
	assert.Equal(t, resp.Link.Notify, true)
}

func TestCreateLink_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
	taskResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.CreateLink(userB, &xagentv1.CreateLinkRequest{
		TaskId:    taskResp.Task.Id,
		Relevance: "Sneaky link",
		Url:       "https://github.com/example/repo/pull/123",
	})

	// Assert
	assert.ErrorContains(t, err, "not found")
}

func TestListLinks(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")
	taskResp, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task with Links",
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
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
	taskResp, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.CreateLink(userA, &xagentv1.CreateLinkRequest{
		TaskId:    taskResp.Task.Id,
		Relevance: "User A's Link",
		Url:       "https://github.com/example/repo/pull/1",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListLinks(userB, &xagentv1.ListLinksRequest{
		TaskId: taskResp.Task.Id,
	})

	// Assert - User B gets empty list, not an error
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Links), 0)
}

func TestFindLinksByURL(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := withUserID(t, "test-user")
	task1, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task 1",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	task2, err := srv.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Name:      "Task 2",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    task1.Task.Id,
		Relevance: "Link from Task 1",
		Url:       "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)
	_, err = srv.CreateLink(ctx, &xagentv1.CreateLinkRequest{
		TaskId:    task2.Task.Id,
		Relevance: "Link from Task 2",
		Url:       "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.FindLinksByURL(ctx, &xagentv1.FindLinksByURLRequest{
		Url: "https://github.com/example/repo/pull/123",
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Links), 2)
}

func TestFindLinksByURL_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := withUserID(t, "user-a")
	userB := withUserID(t, "user-b")
	taskA, err := srv.CreateTask(userA, &xagentv1.CreateTaskRequest{
		Name:      "User A's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	taskB, err := srv.CreateTask(userB, &xagentv1.CreateTaskRequest{
		Name:      "User B's Task",
		Workspace: "test-workspace",
	})
	assert.NilError(t, err)
	_, err = srv.CreateLink(userA, &xagentv1.CreateLinkRequest{
		TaskId:    taskA.Task.Id,
		Relevance: "User A's Link",
		Url:       "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)
	_, err = srv.CreateLink(userB, &xagentv1.CreateLinkRequest{
		TaskId:    taskB.Task.Id,
		Relevance: "User B's Link",
		Url:       "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)

	// Act
	respA, err := srv.FindLinksByURL(userA, &xagentv1.FindLinksByURLRequest{
		Url: "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)
	respB, err := srv.FindLinksByURL(userB, &xagentv1.FindLinksByURLRequest{
		Url: "https://github.com/example/repo/pull/123",
	})
	assert.NilError(t, err)

	// Assert - each user only sees their own link
	assert.Equal(t, len(respA.Links), 1)
	assert.Equal(t, len(respB.Links), 1)
}
