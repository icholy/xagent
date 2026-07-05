package apiserver

import (
	"testing"

	"connectrpc.com/connect"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"

	// Blank-imported so their init registers the eventrouter2 schemas that
	// SetRoutingRules validates against (see event_types_test.go).
	_ "github.com/icholy/xagent/internal/server/githubserver"
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
		{Source: "github", Type: "issue_comment", Conditions: []*xagentv1.RuleCondition{{Attr: "body", Op: "prefix", Value: "bot:"}}},
		{Source: "github", Type: "issue_comment", Conditions: []*xagentv1.RuleCondition{{Attr: "mention", Op: "equals", Value: "mybot"}}},
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
		Rules: []*xagentv1.RoutingRule{
			{Source: "github", Type: "issue_comment", Conditions: []*xagentv1.RuleCondition{{Attr: "body", Op: "prefix", Value: "a:"}}},
		},
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.GetRoutingRules(ctxB, &xagentv1.GetRoutingRulesRequest{})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Rules), 0)
}

func TestSetRoutingRules_RejectsInvalid(t *testing.T) {
	t.Parallel()
	// Arrange
	srv := New(Options{Store: teststore.New(t)})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)

	cases := map[string]*xagentv1.RoutingRule{
		"empty type":      {Source: "github"},
		"unknown type":    {Source: "github", Type: "not_a_type"},
		"unknown attr":    {Source: "github", Type: "issue_comment", Conditions: []*xagentv1.RuleCondition{{Attr: "nope", Op: "equals", Value: "x"}}},
		"unknown op":      {Source: "github", Type: "issue_comment", Conditions: []*xagentv1.RuleCondition{{Attr: "body", Op: "regex", Value: "x"}}},
		"attr wrong type": {Source: "github", Type: "issue_comment", Conditions: []*xagentv1.RuleCondition{{Attr: "assignee", Op: "equals", Value: "x"}}},
	}
	for name, rule := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := srv.SetRoutingRules(ctx, &xagentv1.SetRoutingRulesRequest{
				Rules: []*xagentv1.RoutingRule{rule},
			})
			assert.Equal(t, connect.CodeOf(err), connect.CodeInvalidArgument)
		})
	}

	// The rejected writes never persisted anything.
	resp, err := srv.GetRoutingRules(ctx, &xagentv1.GetRoutingRulesRequest{})
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Rules), 0)
}
