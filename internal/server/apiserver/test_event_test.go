package apiserver

import (
	"testing"

	"github.com/icholy/xagent/internal/eventrouter"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/store/teststore"
	"google.golang.org/protobuf/testing/protocmp"
	"gotest.tools/v3/assert"

	// Blank-imported so its init registers the eventrouter schemas TestEvent
	// routes against (see event_types_test.go).
	_ "github.com/icholy/xagent/internal/server/githubserver"
)

// TestTestEvent is a sanity check on the dry-run handler: it composes a
// synthetic event, runs the real matcher through the injected router, reports
// the matched rule straight through, and writes nothing.
func TestTestEvent(t *testing.T) {
	t.Parallel()
	st := teststore.New(t)
	srv := New(Options{Store: st, Router: &eventrouter.Router{Store: st}})
	org := teststore.CreateOrg(t, srv.store, nil)
	ctx := createCtx(t, org)
	err := srv.store.SetOrgRoutingRules(ctx, nil, org.OrgID, []model.RoutingRule{
		{Source: "github", Type: "issue_comment", Wakeup: true,
			Conditions: []model.Condition{{Attr: "body", Op: "prefix", Value: "xagent:"}}},
	})
	assert.NilError(t, err)

	resp, err := srv.TestEvent(ctx, &xagentv1.TestEventRequest{
		Source: "github",
		Type:   "issue_comment",
		Attrs:  map[string]string{"body": "xagent: do it", "url": "https://github.com/o/r/issues/1"},
	})
	assert.NilError(t, err)

	// The matched rule is reported straight through, and the dry run never fires.
	assert.Assert(t, !resp.Fired)
	assert.DeepEqual(t, resp.Matches, []*xagentv1.TestEventMatch{
		{OrgId: org.OrgID, RuleIndex: 0, WouldWake: true},
	}, protocmp.Transform())

	// Dry run persists nothing.
	tasks, err := srv.store.ListTasks(ctx, nil, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, len(tasks), 0)
	events, err := srv.store.ListEvents(ctx, nil, 1000, org.OrgID, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(events), 0)
}
