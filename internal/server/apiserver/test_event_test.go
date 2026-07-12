package apiserver

import (
	"testing"

	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"

	// Blank-imported so their init registers the eventrouter schemas TestEvent
	// routes against (see event_types_test.go).
	_ "github.com/icholy/xagent/internal/server/githubserver"
)

// newTestEventServer builds an apiserver with a router wired to the given store,
// as TestEvent requires.
func newTestEventServer(t *testing.T) *Server {
	t.Helper()
	st := teststore.New(t)
	return New(Options{
		Store:  st,
		Router: &eventrouter.Router{Store: st},
	})
}

// countRows returns the number of tasks and events an org holds, so a dry-run
// test can assert TestEvent persisted nothing.
func countRows(t *testing.T, srv *Server, orgID int64) (tasks, events int) {
	t.Helper()
	ts, err := srv.store.ListTasks(t.Context(), nil, orgID)
	assert.NilError(t, err)
	es, err := srv.store.ListEvents(t.Context(), nil, 1000, orgID, nil)
	assert.NilError(t, err)
	return len(ts), len(es)
}

func TestTestEvent_DryRun_Wake(t *testing.T) {
	t.Parallel()
	// Arrange: a rule matching a "xagent:"-prefixed comment body, plus an existing
	// task subscribed to the event URL.
	srv := newTestEventServer(t)
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)
	err := srv.store.SetOrgRoutingRules(ctx, nil, org.OrgID, []model.RoutingRule{
		{Source: "github", Type: "issue_comment", Wakeup: true,
			Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}}},
	})
	assert.NilError(t, err)
	const url = "https://github.com/o/r/issues/1"
	teststore.CreateTask(t, srv.store, org, &teststore.TaskOptions{
		Links: []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})

	tasksBefore, eventsBefore := countRows(t, srv, org.OrgID)

	// Act
	resp, err := srv.TestEvent(ctx, &xagentv1.TestEventRequest{
		Source: "github",
		Type:   "issue_comment",
		Attrs:  map[string]string{"body": "xagent: do it", "url": url},
	})

	// Assert: one match reporting a wake, no create. The report is derived from the
	// matched rule alone — no task resolution.
	assert.NilError(t, err)
	assert.Assert(t, !resp.Fired)
	assert.DeepEqual(t, resp.Matches, []*xagentv1.TestEventMatch{
		{
			OrgId:     org.OrgID,
			RuleIndex: 0,
			WouldWake: true,
		},
	}, protocmp.Transform())

	// Dry run persists nothing.
	tasksAfter, eventsAfter := countRows(t, srv, org.OrgID)
	assert.Equal(t, tasksAfter, tasksBefore)
	assert.Equal(t, eventsAfter, eventsBefore)
}

func TestTestEvent_DryRun_Create(t *testing.T) {
	t.Parallel()
	// Arrange: a create rule with no subscribed link for the URL.
	srv := newTestEventServer(t)
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)
	err := srv.store.SetOrgRoutingRules(ctx, nil, org.OrgID, []model.RoutingRule{
		{Source: "github", Type: "issue_comment",
			Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}},
			Create:     &model.CreateTaskAction{Workspace: "default", Runner: "test-runner"}},
	})
	assert.NilError(t, err)

	tasksBefore, eventsBefore := countRows(t, srv, org.OrgID)

	// Act
	resp, err := srv.TestEvent(ctx, &xagentv1.TestEventRequest{
		Source: "github",
		Type:   "issue_comment",
		Attrs:  map[string]string{"body": "xagent: do it", "url": "https://github.com/o/r/issues/2"},
	})

	// Assert: matched, would create, no wake tasks.
	assert.NilError(t, err)
	assert.DeepEqual(t, resp.Matches, []*xagentv1.TestEventMatch{
		{OrgId: org.OrgID, RuleIndex: 0, WouldCreate: true},
	}, protocmp.Transform())

	// Dry run persists nothing.
	tasksAfter, eventsAfter := countRows(t, srv, org.OrgID)
	assert.Equal(t, tasksAfter, tasksBefore)
	assert.Equal(t, eventsAfter, eventsBefore)
}

func TestTestEvent_DryRun_NoMatch(t *testing.T) {
	t.Parallel()
	// Arrange: a rule that won't match the event body.
	srv := newTestEventServer(t)
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)
	err := srv.store.SetOrgRoutingRules(ctx, nil, org.OrgID, []model.RoutingRule{
		{Source: "github", Type: "issue_comment", Wakeup: true,
			Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "other:"}}},
	})
	assert.NilError(t, err)

	// Act
	resp, err := srv.TestEvent(ctx, &xagentv1.TestEventRequest{
		Source: "github",
		Type:   "issue_comment",
		Attrs:  map[string]string{"body": "xagent: hello", "url": "https://github.com/o/r/issues/3"},
	})

	// Assert: no match reported.
	assert.NilError(t, err)
	assert.Equal(t, len(resp.Matches), 0)
}

func TestTestEvent_ScopedToCallerOrg(t *testing.T) {
	t.Parallel()
	// Arrange: two orgs each with a matching rule + subscribed task on the same
	// URL. A caller in orgA must only see orgA's match.
	srv := newTestEventServer(t)
	orgA := teststore.CreateOrg(t, srv.store, nil)
	orgB := teststore.CreateOrg(t, srv.store, nil)
	ctxA := createCtx(t, orgA)
	const url = "https://github.com/o/r/issues/4"
	rule := []model.RoutingRule{
		{Source: "github", Type: "issue_comment", Wakeup: true,
			Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}}},
	}
	assert.NilError(t, srv.store.SetOrgRoutingRules(ctxA, nil, orgA.OrgID, rule))
	assert.NilError(t, srv.store.SetOrgRoutingRules(ctxA, nil, orgB.OrgID, rule))
	teststore.CreateTask(t, srv.store, orgA, &teststore.TaskOptions{
		Links: []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})
	teststore.CreateTask(t, srv.store, orgB, &teststore.TaskOptions{
		Links: []teststore.LinkOptions{{URL: url, Subscribe: true}},
	})

	// Act
	resp, err := srv.TestEvent(ctxA, &xagentv1.TestEventRequest{
		Source: "github",
		Type:   "issue_comment",
		Attrs:  map[string]string{"body": "xagent: do it", "url": url},
	})

	// Assert: only orgA's match is reported.
	assert.NilError(t, err)
	assert.DeepEqual(t, resp.Matches, []*xagentv1.TestEventMatch{
		{
			OrgId:     orgA.OrgID,
			RuleIndex: 0,
			WouldWake: true,
		},
	}, protocmp.Transform())
}
