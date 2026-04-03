package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestGetRoutingRules_Default(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	resp, err := srv.GetRoutingRules(ctx, &xagentv1.GetRoutingRulesRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Rules), 0)
}

func TestSetAndGetRoutingRules(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	setResp, err := srv.SetRoutingRules(ctx, &xagentv1.SetRoutingRulesRequest{
		Rules: []*xagentv1.RoutingRule{
			{Prefix: "bot:"},
			{Source: "github", Mention: "mybot"},
		},
	})
	assert.NilError(t, err)
	assert.Equal(t, len(setResp.Rules), 2)
	assert.Equal(t, setResp.Rules[0].Prefix, "bot:")
	assert.Equal(t, setResp.Rules[1].Source, "github")
	assert.Equal(t, setResp.Rules[1].Mention, "mybot")

	getResp, err := srv.GetRoutingRules(ctx, &xagentv1.GetRoutingRulesRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(getResp.Rules), 2)
	assert.Equal(t, getResp.Rules[0].Prefix, "bot:")
	assert.Equal(t, getResp.Rules[1].Mention, "mybot")
}

func TestSetRoutingRules_OrgIsolation(t *testing.T) {
	t.Parallel()
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, nil)
	orgB := teststore.CreateOrg(t, srv.store, nil)
	ctxA := createCtx(t, orgA)
	ctxB := createCtx(t, orgB)

	_, err := srv.SetRoutingRules(ctxA, &xagentv1.SetRoutingRulesRequest{
		Rules: []*xagentv1.RoutingRule{{Prefix: "a:"}},
	})
	assert.NilError(t, err)

	// Org B should still have empty rules
	resp, err := srv.GetRoutingRules(ctxB, &xagentv1.GetRoutingRulesRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Rules), 0)
}
