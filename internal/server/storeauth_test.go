package server_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/icholy/xagent/internal/auth/authscope"
	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/server"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestValidateKeyReturnsScopes(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	readOwn, err := authscope.Parse(`task.read:{"task.id":"7"}`)
	assert.NilError(t, err)
	hash := uuid.NewString()
	err = s.CreateKey(t.Context(), nil, &model.Key{
		ID:        uuid.NewString(),
		Name:      "scoped",
		TokenHash: hash,
		OrgID:     org.OrgID,
		Scopes:    authscope.Scopes{readOwn},
	})
	assert.NilError(t, err)

	// Act
	info, err := server.NewStoreKeyValidator(s).ValidateKey(t.Context(), hash)

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, info.Scopes, authscope.Scopes{readOwn})
}

func TestValidateKeyNullScopesDefaultsAdmin(t *testing.T) {
	t.Parallel()
	// Arrange - a key with no scopes column stored (NULL).
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	hash := uuid.NewString()
	err := s.CreateKey(t.Context(), nil, &model.Key{
		ID:        uuid.NewString(),
		Name:      "unscoped",
		TokenHash: hash,
		OrgID:     org.OrgID,
	})
	assert.NilError(t, err)

	// Act
	info, err := server.NewStoreKeyValidator(s).ValidateKey(t.Context(), hash)

	// Assert - a NULL/empty column is treated as admin so the key is never
	// locked out.
	assert.NilError(t, err)
	assert.DeepEqual(t, info.Scopes, authscope.Admin())
}
