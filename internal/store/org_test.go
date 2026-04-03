package store_test

import (
	"encoding/json"
	"testing"

	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestGetOrgRoutingRulesDefault(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	data, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)

	rules, err := eventrouter.UnmarshalRules(data)
	assert.NilError(t, err)
	assert.Equal(t, len(rules), 0)
}

func TestSetAndGetOrgRoutingRules(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	rules := []eventrouter.Rule{
		{Prefix: "bot:"},
		{Source: "github", Mention: "mybot"},
	}
	data, err := eventrouter.MarshalRules(rules)
	assert.NilError(t, err)

	err = s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, data)
	assert.NilError(t, err)

	got, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)

	gotRules, err := eventrouter.UnmarshalRules(got)
	assert.NilError(t, err)
	assert.DeepEqual(t, gotRules, rules)
}

func TestGetRoutingRulesByOrgs(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	orgA := teststore.CreateOrg(t, s, nil)
	orgB := teststore.CreateOrg(t, s, nil)

	rulesA := []eventrouter.Rule{{Prefix: "a:"}}
	dataA, err := eventrouter.MarshalRules(rulesA)
	assert.NilError(t, err)
	err = s.SetOrgRoutingRules(t.Context(), nil, orgA.OrgID, dataA)
	assert.NilError(t, err)

	result, err := s.GetRoutingRulesByOrgs(t.Context(), nil, []int64{orgA.OrgID, orgB.OrgID})
	assert.NilError(t, err)
	assert.Equal(t, len(result), 2)

	// orgA has custom rules
	gotA, err := eventrouter.UnmarshalRules(result[orgA.OrgID])
	assert.NilError(t, err)
	assert.DeepEqual(t, gotA, rulesA)

	// orgB has default empty array
	gotB, err := eventrouter.UnmarshalRules(result[orgB.OrgID])
	assert.NilError(t, err)
	assert.Equal(t, len(gotB), 0)
}

func TestSetOrgRoutingRulesOverwrite(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	// Set initial rules
	rules1 := []eventrouter.Rule{{Prefix: "old:"}}
	data1, err := eventrouter.MarshalRules(rules1)
	assert.NilError(t, err)
	err = s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, data1)
	assert.NilError(t, err)

	// Overwrite with new rules
	rules2 := []eventrouter.Rule{{Mention: "newbot"}}
	data2, err := eventrouter.MarshalRules(rules2)
	assert.NilError(t, err)
	err = s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, data2)
	assert.NilError(t, err)

	got, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	gotRules, err := eventrouter.UnmarshalRules(got)
	assert.NilError(t, err)
	assert.DeepEqual(t, gotRules, rules2)
}

func TestSetOrgRoutingRulesClearToEmpty(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	// Set rules
	rules := []eventrouter.Rule{{Prefix: "test:"}}
	data, err := eventrouter.MarshalRules(rules)
	assert.NilError(t, err)
	err = s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, data)
	assert.NilError(t, err)

	// Clear to empty
	err = s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, json.RawMessage("[]"))
	assert.NilError(t, err)

	got, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)
	assert.NilError(t, err)
	gotRules, err := eventrouter.UnmarshalRules(got)
	assert.NilError(t, err)
	assert.Equal(t, len(gotRules), 0)
}
