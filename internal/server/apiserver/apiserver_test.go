package apiserver

import (
	"context"
	"testing"

	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/store/teststore"
)

func createCtx(t *testing.T, org *teststore.Org) context.Context {
	t.Helper()
	return apiauth.WithUser(t.Context(), &apiauth.UserInfo{ID: org.UserID, OrgID: org.OrgID})
}
