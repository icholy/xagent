package eventrouter

import (
	"log/slog"
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"

	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

// setOrgRules is a test helper to set routing rules for an org.
func setOrgRules(t *testing.T, s *store.Store, orgID int64, rules []Rule) {
	t.Helper()
	if err := s.SetOrgRoutingRules(t.Context(), nil, orgID, rules); err != nil {
		t.Fatal(err)
	}
}

func TestRouteCreatesEventAndStartsTask(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	task := teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})

	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	n, err := r.Route(t.Context(), InputEvent{
		Source:      "github",
		Description: "testuser commented on PR #1",
		Data:        "xagent: fix tests",
		URL:         url,
		UserID:      org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)

	// Task was started
	updated, err := s.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, updated.Status, model.TaskStatusPending)
}

func TestRouteMultipleOrgs(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	orgA := teststore.CreateOrg(t, s, nil)
	orgB := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	teststore.CreateTask(t, s, orgA, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	teststore.CreateTask(t, s, orgB, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})

	// Add user A as member of org B so FindSubscribedLinksByURLForUser finds both
	err := s.AddOrgMember(t.Context(), nil, &model.OrgMember{
		OrgID:  orgB.OrgID,
		UserID: orgA.UserID,
		Role:   "member",
	})
	assert.NilError(t, err)

	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do something",
		URL:    url,
		UserID: orgA.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 2)
}

func TestRouteDeduplicatesTasksWithMultipleLinks(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links: []teststore.LinkOptions{
			{URL: url, Subscribe: true},
			{URL: url, Subscribe: true},
		},
	})

	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do something",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
}

func TestRouteNoMatchingLinks(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do something",
		URL:    "https://github.com/owner/repo/pull/1",
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
}

func TestRouteEmptyURL(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do something",
		URL:    "",
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
}

func TestRouteSkipsEventsWithoutXAgentPrefix(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})

	r := &Router{
		Log:   slog.Default(),
		Store: s,
	}

	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "just a regular comment",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 0)
}

func TestRouteWithCustomOrgRules(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})

	// Set custom rules that use a different prefix
	setOrgRules(t, s, org.OrgID, []Rule{{Prefix: "bot:"}})

	r := &Router{Log: slog.Default(), Store: s}

	// Default xagent: prefix should not match
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: fix tests",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 0)

	// Custom prefix should match
	n, err = r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "bot: fix tests",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
}

func TestRouteWithMentionRule(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})

	setOrgRules(t, s, org.OrgID, []Rule{{Source: "github", Mention: "mybot"}})

	r := &Router{Log: slog.Default(), Store: s}

	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "hey @mybot fix this",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
}

func TestRoutePerOrgRulesIsolation(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	orgA := teststore.CreateOrg(t, s, nil)
	orgB := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	teststore.CreateTask(t, s, orgA, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	teststore.CreateTask(t, s, orgB, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})

	// Add user A as member of org B
	err := s.AddOrgMember(t.Context(), nil, &model.OrgMember{
		OrgID:  orgB.OrgID,
		UserID: orgA.UserID,
		Role:   "member",
	})
	assert.NilError(t, err)

	// Org A uses custom prefix, org B uses default
	setOrgRules(t, s, orgA.OrgID, []Rule{{Prefix: "custom:"}})

	r := &Router{Log: slog.Default(), Store: s}

	// xagent: prefix should only route to org B (default rules)
	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: do something",
		URL:    url,
		UserID: orgA.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)

	// custom: prefix should only route to org A
	n, err = r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "custom: do something",
		URL:    url,
		UserID: orgA.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
}

func TestRouteDefaultRulesFallback(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	url := "https://github.com/owner/repo/pull/1"
	teststore.CreateTask(t, s, org, &teststore.TaskOptions{
		Status: model.TaskStatusCompleted,
		Links:  []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})

	// No custom rules set — should use default xagent: prefix
	r := &Router{Log: slog.Default(), Store: s}

	n, err := r.Route(t.Context(), InputEvent{
		Source: "github",
		Data:   "xagent: fix it",
		URL:    url,
		UserID: org.UserID,
	})
	assert.NilError(t, err)
	assert.Equal(t, n, 1)
}
