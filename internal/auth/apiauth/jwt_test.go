package apiauth

import (
	"testing"

	"github.com/icholy/xagent/internal/auth/authscope"
	"gotest.tools/v3/assert"
)

func TestAppClaimsScopesRoundTrip(t *testing.T) {
	t.Parallel()
	// Arrange: mint admin claims for a user.
	key, err := CreateAppPrivateKey()
	assert.NilError(t, err)
	claims := NewAppClaims(&UserInfo{ID: "u1", OrgID: 7})
	assert.DeepEqual(t, claims.Scopes, []string{authscope.AdminScope})

	// Act: sign then verify, mirroring the mint → verify path.
	token, err := SignAppToken(key, claims)
	assert.NilError(t, err)
	verified, err := VerifyAppToken(key, token)
	assert.NilError(t, err)

	// Assert: the scopes claim survives and parses into a Set that authorizes
	// any target, exactly as authenticate populates UserInfo.Scopes.
	assert.DeepEqual(t, verified.Scopes, []string{authscope.AdminScope})
	set, err := authscope.ParseSet(verified.Scopes)
	assert.NilError(t, err)
	user := &UserInfo{Scopes: set}
	assert.Assert(t, user.Authorize(authscope.Target{
		Op:    []string{"task", "write"},
		Attrs: map[string]string{"id": "1"},
	}))
}
