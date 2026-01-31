package server

import (
	"testing"

	"github.com/icholy/xagent/internal/apiauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"gotest.tools/v3/assert"
)

func TestCreateKey(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := randomUserID(t)

	// Act
	resp, err := srv.CreateKey(ctx, &xagentv1.CreateKeyRequest{
		Name: "test-key",
	})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, resp.Key.Name, "test-key")
	assert.Assert(t, resp.Key.Id != "")
	assert.Assert(t, resp.RawToken != "")
	assert.Assert(t, apiauth.IsKey(resp.RawToken))
}

func TestCreateAndListKeys(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := randomUserID(t)
	_, err := srv.CreateKey(ctx, &xagentv1.CreateKeyRequest{
		Name: "key-1",
	})
	assert.NilError(t, err)
	_, err = srv.CreateKey(ctx, &xagentv1.CreateKeyRequest{
		Name: "key-2",
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.ListKeys(ctx, &xagentv1.ListKeysRequest{})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Keys), 2)
}

func TestDeleteKey(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	ctx := randomUserID(t)
	createResp, err := srv.CreateKey(ctx, &xagentv1.CreateKeyRequest{
		Name: "to-delete",
	})
	assert.NilError(t, err)

	// Act
	_, err = srv.DeleteKey(ctx, &xagentv1.DeleteKeyRequest{
		Id: createResp.Key.Id,
	})
	assert.NilError(t, err)

	// Assert
	listResp, err := srv.ListKeys(ctx, &xagentv1.ListKeysRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.Keys), 0)
}

func TestListKeys_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := randomUserID(t)
	userB := randomUserID(t)
	_, err := srv.CreateKey(userA, &xagentv1.CreateKeyRequest{
		Name: "user-a-key",
	})
	assert.NilError(t, err)
	_, err = srv.CreateKey(userB, &xagentv1.CreateKeyRequest{
		Name: "user-b-key",
	})
	assert.NilError(t, err)

	// Act
	respA, err := srv.ListKeys(userA, &xagentv1.ListKeysRequest{})
	assert.NilError(t, err)
	respB, err := srv.ListKeys(userB, &xagentv1.ListKeysRequest{})
	assert.NilError(t, err)

	// Assert - each user only sees their own keys
	assert.Equal(t, len(respA.Keys), 1)
	assert.Equal(t, respA.Keys[0].Name, "user-a-key")
	assert.Equal(t, len(respB.Keys), 1)
	assert.Equal(t, respB.Keys[0].Name, "user-b-key")
}

func TestDeleteKey_Permissions(t *testing.T) {
	// Arrange
	srv := setupTestServer(t)
	userA := randomUserID(t)
	userB := randomUserID(t)
	createResp, err := srv.CreateKey(userA, &xagentv1.CreateKeyRequest{
		Name: "user-a-key",
	})
	assert.NilError(t, err)

	// Act - User B tries to delete User A's key
	_, err = srv.DeleteKey(userB, &xagentv1.DeleteKeyRequest{
		Id: createResp.Key.Id,
	})

	// Assert - delete doesn't error (SQL DELETE with no rows is not an error)
	// but the key should still exist for User A
	assert.NilError(t, err)
	listResp, err := srv.ListKeys(userA, &xagentv1.ListKeysRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(listResp.Keys), 1)
}
