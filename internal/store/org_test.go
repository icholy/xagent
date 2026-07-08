package store_test

import (
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// findOrgRules returns the entry for orgID in result, failing if it is missing
// or appears more than once.
func findOrgRules(t *testing.T, result []store.OrgRoutingRules, orgID int64) store.OrgRoutingRules {
	t.Helper()
	var found *store.OrgRoutingRules
	for i := range result {
		if result[i].OrgID == orgID {
			if found != nil {
				t.Fatalf("org %d appears more than once in result", orgID)
			}
			found = &result[i]
		}
	}
	if found == nil {
		t.Fatalf("org %d not found in result", orgID)
	}
	return *found
}

func TestGetOrgRoutingRulesDefault(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	// Act
	rules, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(rules), 0)
}

func TestSetAndGetOrgRoutingRules(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	rules := []model.RoutingRule{
		{Source: "github", Type: "issue_comment", Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "bot:"}}},
		{Source: "github", Type: "issue_comment", Conditions: []model.Condition{{Attr: "mention", Op: "equals", Value: "mybot"}}},
	}
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, rules)
	assert.NilError(t, err)

	// Act
	got, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, got, rules)
}

func TestGetRoutingRulesByOrgs(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	orgA := teststore.CreateOrg(t, s, nil)
	orgB := teststore.CreateOrg(t, s, nil)
	rulesA := []model.RoutingRule{{Source: "github", Type: "issue_comment", Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "a:"}}}}
	err := s.SetOrgRoutingRules(t.Context(), nil, orgA.OrgID, rulesA)
	assert.NilError(t, err)

	// Act
	result, err := s.GetRoutingRulesByOrgs(t.Context(), nil, []int64{orgA.OrgID, orgB.OrgID})

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(result), 2)
	assert.DeepEqual(t, result[orgA.OrgID], rulesA)
	assert.Equal(t, len(result[orgB.OrgID]), 0)
}

func TestSetOrgRoutingRulesOverwrite(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	rules1 := []model.RoutingRule{{Source: "github", Type: "issue_comment", Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "old:"}}}}
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, rules1)
	assert.NilError(t, err)
	rules2 := []model.RoutingRule{{Source: "github", Type: "issue_comment", Conditions: []model.Condition{{Attr: "mention", Op: "equals", Value: "newbot"}}}}
	err = s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, rules2)
	assert.NilError(t, err)

	// Act
	got, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, got, rules2)
}

func TestListRoutingRulesForEvent(t *testing.T) {
	t.Parallel()
	// Arrange: a member org (the actor belongs to it) and a distinct org the
	// actor is NOT a member of, each with its own rules.
	s := teststore.New(t)
	memberOrg := teststore.CreateOrg(t, s, nil)
	memberRules := []model.RoutingRule{{Source: "github", Type: "issue_comment", Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "m:"}}}}
	assert.NilError(t, s.SetOrgRoutingRules(t.Context(), nil, memberOrg.OrgID, memberRules))

	nonMemberOrg := teststore.CreateOrg(t, s, nil)
	nonMemberRules := []model.RoutingRule{{Source: "github", Type: "issue_comment", Public: true}}
	assert.NilError(t, s.SetOrgRoutingRules(t.Context(), nil, nonMemberOrg.OrgID, nonMemberRules))

	// Act
	result, err := s.ListRoutingRulesForEvent(t.Context(), nil, memberOrg.UserID, []int64{nonMemberOrg.OrgID})

	// Assert: the member org is tagged IsMember=true with all its rules; the
	// passed org is tagged IsMember=false with its rules (the store returns a
	// faithful view — the public-flag filter is the router's job).
	assert.NilError(t, err)
	member := findOrgRules(t, result, memberOrg.OrgID)
	assert.Equal(t, member.IsMember, true)
	assert.DeepEqual(t, member.Rules, memberRules)

	nonMember := findOrgRules(t, result, nonMemberOrg.OrgID)
	assert.Equal(t, nonMember.IsMember, false)
	assert.DeepEqual(t, nonMember.Rules, nonMemberRules)
}

func TestListRoutingRulesForEventMembershipWins(t *testing.T) {
	t.Parallel()
	// Arrange: an org the actor is a member of, also named in the passed orgs.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	rules := []model.RoutingRule{{Source: "github", Type: "issue_comment", Public: true}}
	assert.NilError(t, s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, rules))

	// Act: pass the member org's own id — the overlap should resolve in favor of
	// membership (returned once, from the member branch).
	result, err := s.ListRoutingRulesForEvent(t.Context(), nil, org.UserID, []int64{org.OrgID})

	// Assert
	assert.NilError(t, err)
	got := findOrgRules(t, result, org.OrgID) // fails if it appears twice
	assert.Equal(t, got.IsMember, true)
	assert.DeepEqual(t, got.Rules, rules)
}

func TestListRoutingRulesForEventExcludesArchived(t *testing.T) {
	t.Parallel()
	// Arrange: an archived member org and an archived non-member org.
	s := teststore.New(t)
	memberOrg := teststore.CreateOrg(t, s, nil)
	assert.NilError(t, s.ArchiveOrg(t.Context(), nil, memberOrg.OrgID))
	nonMemberOrg := teststore.CreateOrg(t, s, nil)
	assert.NilError(t, s.ArchiveOrg(t.Context(), nil, nonMemberOrg.OrgID))

	// Act
	result, err := s.ListRoutingRulesForEvent(t.Context(), nil, memberOrg.UserID, []int64{nonMemberOrg.OrgID})

	// Assert: archived orgs are excluded from both branches.
	assert.NilError(t, err)
	for _, o := range result {
		assert.Assert(t, o.OrgID != memberOrg.OrgID, "archived member org should be excluded")
		assert.Assert(t, o.OrgID != nonMemberOrg.OrgID, "archived non-member org should be excluded")
	}
}

func TestListRoutingRulesForEventEmptyOrgs(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	memberOrg := teststore.CreateOrg(t, s, nil)
	rules := []model.RoutingRule{{Source: "github", Type: "issue_comment"}}
	assert.NilError(t, s.SetOrgRoutingRules(t.Context(), nil, memberOrg.OrgID, rules))

	// Act: an empty orgs slice reduces to the member-only set.
	result, err := s.ListRoutingRulesForEvent(t.Context(), nil, memberOrg.UserID, nil)

	// Assert
	assert.NilError(t, err)
	member := findOrgRules(t, result, memberOrg.OrgID)
	assert.Equal(t, member.IsMember, true)
	assert.DeepEqual(t, member.Rules, rules)
}

func TestListRoutingRulesForEventEmptyUser(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	rules := []model.RoutingRule{{Source: "github", Type: "issue_comment", Public: true}}
	assert.NilError(t, s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, rules))

	// Act: an empty user id yields just the non-member branch.
	result, err := s.ListRoutingRulesForEvent(t.Context(), nil, "", []int64{org.OrgID})

	// Assert
	assert.NilError(t, err)
	got := findOrgRules(t, result, org.OrgID)
	assert.Equal(t, got.IsMember, false)
	assert.DeepEqual(t, got.Rules, rules)
}

func TestSetOrgRoutingRulesClearToEmpty(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	rules := []model.RoutingRule{{Source: "github", Type: "issue_comment", Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "test:"}}}}
	err := s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, rules)
	assert.NilError(t, err)
	err = s.SetOrgRoutingRules(t.Context(), nil, org.OrgID, nil)
	assert.NilError(t, err)

	// Act
	got, err := s.GetOrgRoutingRules(t.Context(), nil, org.OrgID)

	// Assert
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}
