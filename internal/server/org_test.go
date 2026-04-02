package server

import (
	"testing"

	"github.com/icholy/xagent/internal/apiauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"gotest.tools/v3/assert"
)

func TestCreateOrg(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctx, _ := createTestOrg(t, srv, nil)

	resp, err := srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{
		Name: "my-org",
	})

	assert.NilError(t, err)
	assert.Equal(t, resp.Org.Name, "my-org")
	assert.Assert(t, resp.Org.Id != 0)
}

func TestListOrgs(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctx, _ := createTestOrg(t, srv, nil)
	_, err := srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{Name: "org-1"})
	assert.NilError(t, err)
	_, err = srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{Name: "org-2"})
	assert.NilError(t, err)

	resp, err := srv.ListOrgs(ctx, &xagentv1.ListOrgsRequest{})

	assert.NilError(t, err)
	// 2 created + 1 from createTestOrg
	assert.Equal(t, len(resp.Orgs), 3)
}

func TestDeleteOrg(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctx, _ := createTestOrg(t, srv, nil)
	createResp, err := srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{Name: "to-delete"})
	assert.NilError(t, err)

	_, err = srv.DeleteOrg(ctx, &xagentv1.DeleteOrgRequest{Id: createResp.Org.Id})
	assert.NilError(t, err)

	resp, err := srv.ListOrgs(ctx, &xagentv1.ListOrgsRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Orgs), 1)
}

func TestDeleteOrg_Permissions(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctxA, _ := createTestOrg(t, srv, nil)
	ctxB, _ := createTestOrg(t, srv, nil)
	createResp, err := srv.CreateOrg(ctxA, &xagentv1.CreateOrgRequest{Name: "user-a-org"})
	assert.NilError(t, err)

	_, err = srv.DeleteOrg(ctxB, &xagentv1.DeleteOrgRequest{Id: createResp.Org.Id})

	assert.ErrorContains(t, err, "only the org owner can delete it")
}

func TestDeleteOrg_DefaultOrg(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctx, _ := createTestOrg(t, srv, nil)
	user := apiauth.Caller(ctx)

	_, err := srv.DeleteOrg(ctx, &xagentv1.DeleteOrgRequest{Id: user.OrgID})

	assert.ErrorContains(t, err, "cannot delete your default org")
}

func TestAddAndListOrgMembers(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	owner, _ := createTestOrg(t, srv, nil)
	member, _ := createTestOrg(t, srv, nil)
	memberEmail := apiauth.Caller(member).ID + "@test.com"

	resp, err := srv.AddOrgMember(owner, &xagentv1.AddOrgMemberRequest{
		Email: memberEmail,
	})
	assert.NilError(t, err)
	assert.Equal(t, resp.Member.Role, "member")

	listResp, err := srv.ListOrgMembers(owner, &xagentv1.ListOrgMembersRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.Members), 2)
}

func TestDeleteOrg_WithTasks(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	ctx, _ := createTestOrg(t, srv, nil)

	// Create a second org and switch to it.
	createResp, err := srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{Name: "has-tasks"})
	assert.NilError(t, err)
	orgCtx := apiauth.WithUser(ctx, &apiauth.UserInfo{
		ID:    apiauth.Caller(ctx).ID,
		OrgID: createResp.Org.Id,
	})

	// Register workspaces for the new org.
	_, err = srv.RegisterWorkspaces(orgCtx, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "test-runner",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "default"},
		},
	})
	assert.NilError(t, err)

	// Create a task in that org.
	_, err = srv.CreateTask(orgCtx, &xagentv1.CreateTaskRequest{
		Runner:    "test-runner",
		Workspace: "default",
	})
	assert.NilError(t, err)

	// Deleting the org should succeed (soft delete).
	_, err = srv.DeleteOrg(ctx, &xagentv1.DeleteOrgRequest{Id: createResp.Org.Id})
	assert.NilError(t, err)

	// The org should no longer appear in the list.
	listResp, err := srv.ListOrgs(ctx, &xagentv1.ListOrgsRequest{})
	assert.NilError(t, err)
	for _, org := range listResp.Orgs {
		assert.Assert(t, org.Id != createResp.Org.Id)
	}
}

func TestRemoveOrgMember(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)
	owner, _ := createTestOrg(t, srv, nil)
	member, _ := createTestOrg(t, srv, nil)
	memberUser := apiauth.Caller(member)
	memberEmail := memberUser.ID + "@test.com"

	_, err := srv.AddOrgMember(owner, &xagentv1.AddOrgMemberRequest{
		Email: memberEmail,
	})
	assert.NilError(t, err)

	_, err = srv.RemoveOrgMember(owner, &xagentv1.RemoveOrgMemberRequest{
		UserId: memberUser.ID,
	})
	assert.NilError(t, err)

	listResp, err := srv.ListOrgMembers(owner, &xagentv1.ListOrgMembersRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.Members), 1)
}
