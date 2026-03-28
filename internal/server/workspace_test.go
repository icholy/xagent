package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"gotest.tools/v3/assert"
)

func TestRegisterWorkspaces(t *testing.T) {
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

	// Register workspaces
	_, err := srv.RegisterWorkspaces(ctx, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "workspace-a"},
			{Name: "workspace-b"},
		},
	})
	assert.NilError(t, err)

	// List workspaces
	listResp, err := srv.ListWorkspaces(ctx, &xagentv1.ListWorkspacesRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.Workspaces), 2)
	assert.Equal(t, listResp.Workspaces[0].Name, "workspace-a")
	assert.Equal(t, listResp.Workspaces[1].Name, "workspace-b")
}

func TestRegisterWorkspaces_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)

	// User A registers workspaces
	_, err := srv.RegisterWorkspaces(userA, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "workspace-a"},
		},
	})
	assert.NilError(t, err)

	// User B registers different workspaces
	_, err = srv.RegisterWorkspaces(userB, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "workspace-b"},
		},
	})
	assert.NilError(t, err)

	// Act
	listRespA, errA := srv.ListWorkspaces(userA, &xagentv1.ListWorkspacesRequest{})
	listRespB, errB := srv.ListWorkspaces(userB, &xagentv1.ListWorkspacesRequest{})

	// Assert - each user should only see their own workspaces
	assert.NilError(t, errA)
	assert.NilError(t, errB)
	assert.Equal(t, len(listRespA.Workspaces), 1)
	assert.Equal(t, listRespA.Workspaces[0].Name, "workspace-a")
	assert.Equal(t, len(listRespB.Workspaces), 1)
	assert.Equal(t, listRespB.Workspaces[0].Name, "workspace-b")
}

func TestRegisterWorkspaces_SameRunnerDifferentUsers(t *testing.T) {
	// Arrange - both users register workspaces for the same runner ID
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)

	// User A registers workspaces for runner-1
	_, err := srv.RegisterWorkspaces(userA, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "workspace-a"},
		},
	})
	assert.NilError(t, err)

	// User B registers workspaces for the same runner-1
	_, err = srv.RegisterWorkspaces(userB, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "workspace-b"},
		},
	})
	assert.NilError(t, err)

	// Act - User A re-registers (should only delete their own workspaces)
	_, err = srv.RegisterWorkspaces(userA, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "workspace-c"},
		},
	})
	assert.NilError(t, err)

	// Assert - User A should see workspace-c, User B should still see workspace-b
	listRespA, err := srv.ListWorkspaces(userA, &xagentv1.ListWorkspacesRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listRespA.Workspaces), 1)
	assert.Equal(t, listRespA.Workspaces[0].Name, "workspace-c")

	listRespB, err := srv.ListWorkspaces(userB, &xagentv1.ListWorkspacesRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listRespB.Workspaces), 1)
	assert.Equal(t, listRespB.Workspaces[0].Name, "workspace-b")
}

func TestClearWorkspaces(t *testing.T) {
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

	// Register workspaces
	_, err := srv.RegisterWorkspaces(ctx, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "workspace-a"},
			{Name: "workspace-b"},
		},
	})
	assert.NilError(t, err)

	// Verify workspaces exist
	listResp, err := srv.ListWorkspaces(ctx, &xagentv1.ListWorkspacesRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.Workspaces), 2)

	// Clear workspaces
	_, err = srv.ClearWorkspaces(ctx, &xagentv1.ClearWorkspacesRequest{})
	assert.NilError(t, err)

	// Verify workspaces are cleared
	listResp, err = srv.ListWorkspaces(ctx, &xagentv1.ListWorkspacesRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.Workspaces), 0)
}

func TestClearWorkspaces_Permissions(t *testing.T) {
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)

	// User A registers workspaces
	_, err := srv.RegisterWorkspaces(userA, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "workspace-a"},
		},
	})
	assert.NilError(t, err)

	// User B registers workspaces
	_, err = srv.RegisterWorkspaces(userB, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "runner-1",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "workspace-b"},
		},
	})
	assert.NilError(t, err)

	// User A clears workspaces - should only clear their own
	_, err = srv.ClearWorkspaces(userA, &xagentv1.ClearWorkspacesRequest{})
	assert.NilError(t, err)

	// Verify User A has no workspaces
	listRespA, err := srv.ListWorkspaces(userA, &xagentv1.ListWorkspacesRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listRespA.Workspaces), 0)

	// Verify User B still has their workspaces
	listRespB, err := srv.ListWorkspaces(userB, &xagentv1.ListWorkspacesRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listRespB.Workspaces), 1)
	assert.Equal(t, listRespB.Workspaces[0].Name, "workspace-b")
}
