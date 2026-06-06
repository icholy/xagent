package apiauth

import (
	"testing"

	"github.com/icholy/xagent/internal/auth/authscope"
	"gotest.tools/v3/assert"
)

func TestUserInfoAuthorize_Admin(t *testing.T) {
	t.Parallel()
	// Arrange: a caller granted the admin wildcard.
	user := &UserInfo{Scopes: authscope.Admin()}

	// Act & Assert: admin authorizes any 2-segment target.
	assert.Assert(t, user.Authorize(authscope.Target{
		Op:    []string{"task", "read"},
		Attrs: []authscope.Attr{authscope.StringAttr("id", "1")},
	}))
	assert.Assert(t, user.Authorize(authscope.Target{
		Op: []string{"github_token", "create"},
	}))
}

func TestUserInfoAuthorize_NoScopes(t *testing.T) {
	t.Parallel()
	// Arrange: a caller carrying no scopes.
	user := &UserInfo{}

	// Act & Assert: an empty set authorizes nothing.
	assert.Assert(t, !user.Authorize(authscope.Target{Op: []string{"task", "read"}}))
}
