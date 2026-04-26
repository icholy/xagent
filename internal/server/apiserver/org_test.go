package apiserver

import (
	"testing"

	"github.com/icholy/xagent/internal/auth/apiauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestCreateOrg(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	// Act
	resp, err := srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{
		Name: "my-org",
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, resp.Org.Name, "my-org")
	assert.Assert(t, resp.Org.Id != 0)
}

func TestListOrgs(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)
	_, err := srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{Name: "org-1"})
	assert.NilError(t, err)
	_, err = srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{Name: "org-2"})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListOrgs(ctx, &xagentv1.ListOrgsRequest{})

	// Assert
	assert.NilError(t, err)
	// 2 created + 1 from CreateOrg
	assert.Equal(t, len(resp.Orgs), 3)
}

func TestDeleteOrg(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)
	createResp, err := srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{Name: "to-delete"})
	assert.NilError(t, err)

	// Act
	_, err = srv.DeleteOrg(ctx, &xagentv1.DeleteOrgRequest{Id: createResp.Org.Id})

	// Assert
	assert.NilError(t, err)
	resp, err := srv.ListOrgs(ctx, &xagentv1.ListOrgsRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Orgs), 1)
}

func TestDeleteOrg_Permissions(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, nil)
	ctxA := createCtx(t, orgA)
	orgB := teststore.CreateOrg(t, srv.store, nil)
	ctxB := createCtx(t, orgB)
	createResp, err := srv.CreateOrg(ctxA, &xagentv1.CreateOrgRequest{Name: "user-a-org"})
	assert.NilError(t, err)

	// Act
	_, err = srv.DeleteOrg(ctxB, &xagentv1.DeleteOrgRequest{Id: createResp.Org.Id})

	// Assert
	assert.ErrorContains(t, err, "only the org owner can delete it")
}

func TestDeleteOrg_DefaultOrg(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	// Act
	_, err := srv.DeleteOrg(ctx, &xagentv1.DeleteOrgRequest{Id: org.OrgID})

	// Assert
	assert.ErrorContains(t, err, "cannot delete your default org")
}

func TestAddAndListOrgMembers(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	ownerOrg := teststore.CreateOrg(t, srv.store, nil)
	owner := createCtx(t, ownerOrg)
	memberOrg := teststore.CreateOrg(t, srv.store, nil)
	member := createCtx(t, memberOrg)
	memberEmail := apiauth.Caller(member).ID + "@test.com"

	// Act
	resp, err := srv.AddOrgMember(owner, &xagentv1.AddOrgMemberRequest{
		Email: memberEmail,
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, resp.Member.Role, "member")
	listResp, err := srv.ListOrgMembers(owner, &xagentv1.ListOrgMembersRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.Members), 2)
}

func TestAddOrgMember_Duplicate(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	ownerOrg := teststore.CreateOrg(t, srv.store, nil)
	owner := createCtx(t, ownerOrg)
	memberOrg := teststore.CreateOrg(t, srv.store, nil)
	member := createCtx(t, memberOrg)
	memberEmail := apiauth.Caller(member).ID + "@test.com"

	// Act
	_, err := srv.AddOrgMember(owner, &xagentv1.AddOrgMemberRequest{
		Email: memberEmail,
	})
	assert.NilError(t, err)

	// Add the same member again
	_, err = srv.AddOrgMember(owner, &xagentv1.AddOrgMemberRequest{
		Email: memberEmail,
	})

	// Assert
	assert.ErrorContains(t, err, "already a member")
}

func TestDeleteOrg_WithTasks(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)
	createResp, err := srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{Name: "has-tasks"})
	assert.NilError(t, err)
	orgCtx := apiauth.WithUser(ctx, &apiauth.UserInfo{
		ID:    apiauth.Caller(ctx).ID,
		OrgID: createResp.Org.Id,
	})
	_, err = srv.RegisterWorkspaces(orgCtx, &xagentv1.RegisterWorkspacesRequest{
		RunnerId: "test-runner",
		Workspaces: []*xagentv1.RegisteredWorkspace{
			{Name: "default"},
		},
	})
	assert.NilError(t, err)
	_, err = srv.CreateTask(orgCtx, &xagentv1.CreateTaskRequest{
		Runner:    "test-runner",
		Workspace: "default",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.DeleteOrg(ctx, &xagentv1.DeleteOrgRequest{Id: createResp.Org.Id})

	// Assert
	assert.NilError(t, err)
	listResp, err := srv.ListOrgs(ctx, &xagentv1.ListOrgsRequest{})
	assert.NilError(t, err)
	for _, o := range listResp.Orgs {
		assert.Assert(t, o.Id != createResp.Org.Id)
	}
}

func TestRemoveOrgMember(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	ownerOrg := teststore.CreateOrg(t, srv.store, nil)
	owner := createCtx(t, ownerOrg)
	memberOrg := teststore.CreateOrg(t, srv.store, nil)
	memberUser := apiauth.Caller(createCtx(t, memberOrg))
	memberEmail := memberUser.ID + "@test.com"
	_, err := srv.AddOrgMember(owner, &xagentv1.AddOrgMemberRequest{
		Email: memberEmail,
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.RemoveOrgMember(owner, &xagentv1.RemoveOrgMemberRequest{
		UserId: memberUser.ID,
	})

	// Assert
	assert.NilError(t, err)
	listResp, err := srv.ListOrgMembers(owner, &xagentv1.ListOrgMembersRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.Members), 1)
}
