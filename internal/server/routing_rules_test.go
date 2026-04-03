package server

import (
	"testing"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"
)

func TestGetRoutingRules_Default(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	// Act
	resp, err := srv.GetRoutingRules(ctx, &xagentv1.GetRoutingRulesRequest{})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Rules), 0)
}

func TestSetAndGetRoutingRules(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)
	rules := []*xagentv1.RoutingRule{
		{Prefix: "bot:"},
		{Source: "github", Mention: "mybot"},
	}

	// Act
	setResp, err := srv.SetRoutingRules(ctx, &xagentv1.SetRoutingRulesRequest{
		Rules: rules,
	})

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, setResp.Rules, rules, protocmp.Transform())

	// Act
	getResp, err := srv.GetRoutingRules(ctx, &xagentv1.GetRoutingRulesRequest{})

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, getResp.Rules, rules, protocmp.Transform())
}

func TestSetRoutingRules_OrgIsolation(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	orgA := teststore.CreateOrg(t, srv.store, nil)
	orgB := teststore.CreateOrg(t, srv.store, nil)
	ctxA := createCtx(t, orgA)
	ctxB := createCtx(t, orgB)
	_, err := srv.SetRoutingRules(ctxA, &xagentv1.SetRoutingRulesRequest{
		Rules: []*xagentv1.RoutingRule{{Prefix: "a:"}},
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.GetRoutingRules(ctxB, &xagentv1.GetRoutingRulesRequest{})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Rules), 0)
}
