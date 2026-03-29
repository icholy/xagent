package server

import (
	"testing"

	"github.com/icholy/xagent/internal/apiauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"gotest.tools/v3/assert"
)

func TestCreateOrg(t *testing.T) {
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)

	resp, err := srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{
		Name: "my-org",
	})

	assert.NilError(t, err)
	assert.Equal(t, resp.Org.Name, "my-org")
	assert.Assert(t, resp.Org.Id != 0)
}

func TestListOrgs(t *testing.T) {
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)
	_, err := srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{Name: "org-1"})
	assert.NilError(t, err)
	_, err = srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{Name: "org-2"})
	assert.NilError(t, err)

	resp, err := srv.ListOrgs(ctx, &xagentv1.ListOrgsRequest{})

	assert.NilError(t, err)
	// 2 created + 1 from createTestUser
	assert.Equal(t, len(resp.Orgs), 3)
}

func TestDeleteOrg(t *testing.T) {
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)
	createResp, err := srv.CreateOrg(ctx, &xagentv1.CreateOrgRequest{Name: "to-delete"})
	assert.NilError(t, err)

	_, err = srv.DeleteOrg(ctx, &xagentv1.DeleteOrgRequest{Id: createResp.Org.Id})
	assert.NilError(t, err)

	resp, err := srv.ListOrgs(ctx, &xagentv1.ListOrgsRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Orgs), 1)
}

func TestDeleteOrg_Permissions(t *testing.T) {
	srv := setupTestServer(t)
	userA := createTestUser(t, srv)
	userB := createTestUser(t, srv)
	createResp, err := srv.CreateOrg(userA, &xagentv1.CreateOrgRequest{Name: "user-a-org"})
	assert.NilError(t, err)

	_, err = srv.DeleteOrg(userB, &xagentv1.DeleteOrgRequest{Id: createResp.Org.Id})

	assert.ErrorContains(t, err, "only the org owner can delete it")
}

func TestDeleteOrg_DefaultOrg(t *testing.T) {
	srv := setupTestServer(t)
	ctx := createTestUser(t, srv)
	user := apiauth.User(ctx)

	_, err := srv.DeleteOrg(ctx, &xagentv1.DeleteOrgRequest{Id: user.OrgID})

	assert.ErrorContains(t, err, "cannot delete your default org")
}

func TestAddAndListOrgMembers(t *testing.T) {
	srv := setupTestServer(t)
	owner := createTestUser(t, srv)
	member := createTestUser(t, srv)
	memberEmail := apiauth.User(member).ID + "@test.com"

	resp, err := srv.AddOrgMember(owner, &xagentv1.AddOrgMemberRequest{
		Email: memberEmail,
	})
	assert.NilError(t, err)
	assert.Equal(t, resp.Member.Role, "member")

	listResp, err := srv.ListOrgMembers(owner, &xagentv1.ListOrgMembersRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.Members), 2)
}

func TestRemoveOrgMember(t *testing.T) {
	srv := setupTestServer(t)
	owner := createTestUser(t, srv)
	member := createTestUser(t, srv)
	memberUser := apiauth.User(member)
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
