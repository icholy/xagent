package store_test

import (
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestGetOrgRoutingRulesDefault(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	rules, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(rules), 0)
}

func TestSetAndGetOrgRoutingRules(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	rules := []model.RoutingRule{
		{Prefix: "bot:"},
		{Source: "github", Mention: "mybot"},
	}
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, rules)
	assert.NilError(t, err)

	got, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, rules)
}

func TestGetRoutingRulesByOrgs(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	orgA := teststore.CreateOrg(t, s, nil)
	orgB := teststore.CreateOrg(t, s, nil)

	rulesA := []model.RoutingRule{{Prefix: "a:"}}
	err := s.SetOrgRoutingRules(t.Context(), nil, orgA.OrgID, rulesA)
	assert.NilError(t, err)

	result, err := s.GetRoutingRulesByOrgs(t.Context(), nil, []int64{orgA.OrgID, orgB.OrgID})
	assert.NilError(t, err)
	assert.Equal(t, len(result), 2)

	assert.DeepEqual(t, result[orgA.OrgID], rulesA)
	assert.Equal(t, len(result[orgB.OrgID]), 0)
}

func TestSetOrgRoutingRulesOverwrite(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	rules1 := []model.RoutingRule{{Prefix: "old:"}}
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, rules1)
	assert.NilError(t, err)

	rules2 := []model.RoutingRule{{Mention: "newbot"}}
	err = s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, rules2)
	assert.NilError(t, err)

	got, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, rules2)
}

func TestSetOrgRoutingRulesClearToEmpty(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	rules := []model.RoutingRule{{Prefix: "test:"}}
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, rules)
	assert.NilError(t, err)

	err = s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, nil)
	assert.NilError(t, err)

	got, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}
