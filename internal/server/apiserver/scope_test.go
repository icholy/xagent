package apiserver

import (
	"reflect"
	"testing"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// scopeExemptMethods are the only XAgentService methods that do NOT gate on the
// caller's scopes. Each entry is justified; every other method must deny an
// empty-scopes caller with PermissionDenied (asserted by
// TestAllMethodsScopeChecked). Adding a new RPC therefore forces either a real
// scope check or a deliberate, reviewed entry here.
var scopeExemptMethods = map[string]string{
	// Ping carries no caller data and returns the server version unconditionally.
	"Ping": "no caller data; returns version unconditionally",
	// GetProfile acts on the caller's own identity (the identity axis, not an
	// intra-org capability); it is gated by RequireUserInterceptor, not scopes.
	"GetProfile": "identity axis; gated by being an authenticated user, not scopes",
	// CreateGitHubToken is intentionally Unimplemented on the API server; only
	// the runner proxy serves it (#806).
	"CreateGitHubToken": "API server leaves it Unimplemented; served by the runner proxy",
}

// TestAllMethodsScopeChecked is the completeness safety net for the explicit
// per-handler enforcement model (proposal §8). There is no central default-deny
// interceptor, so a newly added RPC that forgets its top-of-handler Allow would
// ship fail-open. This test enumerates every method of XAgentServiceHandler by
// reflection (so the list can't drift from the proto) and asserts that a caller
// with empty scopes is denied with PermissionDenied — except the small, audited
// exempt set, whose actual exemption behavior is asserted instead of skipped.
func TestAllMethodsScopeChecked(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	// A present-but-scopeless caller: tenancy is satisfied (a caller exists) so
	// every handler reaches its scope gate, which must deny.
	ctx := apiauth.WithUser(t.Context(), &apiauth.UserInfo{})

	handlerType := reflect.TypeOf((*xagentv1connect.XAgentServiceHandler)(nil)).Elem()
	srvVal := reflect.ValueOf(srv)
	for i := 0; i < handlerType.NumMethod(); i++ {
		name := handlerType.Method(i).Name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			method := srvVal.MethodByName(name)
			// Interface method signature: func(context.Context, *Req) (*Resp, error).
			// The bound method on srv drops the receiver, so In(1) is the request.
			reqType := method.Type().In(1)
			req := reflect.New(reqType.Elem())
			out := method.Call([]reflect.Value{reflect.ValueOf(ctx), req})
			err, _ := out[1].Interface().(error)

			if reason, exempt := scopeExemptMethods[name]; exempt {
				// Exempt methods must NOT scope-deny an empty-scopes caller.
				assert.Assert(t, connect.CodeOf(err) != connect.CodePermissionDenied,
					"%s is scope-exempt (%s) but returned PermissionDenied", name, reason)
				if name == "CreateGitHubToken" {
					assert.Equal(t, connect.CodeOf(err), connect.CodeUnimplemented)
				}
				return
			}
			assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied,
				"%s must deny an empty-scopes caller", name)
		})
	}
}
