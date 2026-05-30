package archiver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pubsub"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
)

func TestArchiver_Tick_ArchivesEligibleTask(t *testing.T) {
	t.Parallel()
	st := teststore.New(t)
	org := teststore.CreateOrg(t, st, nil)

	task := teststore.CreateTask(t, st, org, &teststore.TaskOptions{Status: model.TaskStatusCompleted})
	// Negative archive_after means archive immediately once terminal, which
	// keeps the test fast and deterministic.
	task.ArchiveAfter = -time.Hour
	assert.NilError(t, st.UpdateTask(t.Context(), nil, task))

	a := New(Options{Store: st})
	assert.NilError(t, a.Tick(t.Context()))

	got, err := st.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Assert(t, got.Archived, "task should be archived after tick")
}

func TestArchiver_Tick_SkipsTaskWithoutArchiveAfter(t *testing.T) {
	t.Parallel()
	st := teststore.New(t)
	org := teststore.CreateOrg(t, st, nil)
	task := teststore.CreateTask(t, st, org, &teststore.TaskOptions{Status: model.TaskStatusCompleted})

	a := New(Options{Store: st})
	assert.NilError(t, a.Tick(t.Context()))

	got, err := st.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Assert(t, !got.Archived, "task without archive_after must not be archived")
}

func TestArchiver_Tick_SkipsNonTerminalTask(t *testing.T) {
	t.Parallel()
	st := teststore.New(t)
	org := teststore.CreateOrg(t, st, nil)

	task := teststore.CreateTask(t, st, org, &teststore.TaskOptions{Status: model.TaskStatusRunning})
	task.ArchiveAfter = -time.Hour
	assert.NilError(t, st.UpdateTask(t.Context(), nil, task))

	a := New(Options{Store: st})
	assert.NilError(t, a.Tick(t.Context()))

	got, err := st.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Assert(t, !got.Archived, "running task must not be auto-archived")
}

func TestArchiver_Tick_SkipsTaskWithPendingCommand(t *testing.T) {
	t.Parallel()
	st := teststore.New(t)
	org := teststore.CreateOrg(t, st, nil)

	task := teststore.CreateTask(t, st, org, &teststore.TaskOptions{Status: model.TaskStatusCompleted})
	task.ArchiveAfter = -time.Hour
	task.Command = model.TaskCommandRestart
	assert.NilError(t, st.UpdateTask(t.Context(), nil, task))

	a := New(Options{Store: st})
	assert.NilError(t, a.Tick(t.Context()))

	got, err := st.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Assert(t, !got.Archived, "task with pending command must not be auto-archived")
}

func TestArchiver_Tick_SkipsFutureDeadline(t *testing.T) {
	t.Parallel()
	st := teststore.New(t)
	org := teststore.CreateOrg(t, st, nil)

	task := teststore.CreateTask(t, st, org, &teststore.TaskOptions{Status: model.TaskStatusCompleted})
	task.ArchiveAfter = time.Hour
	assert.NilError(t, st.UpdateTask(t.Context(), nil, task))

	a := New(Options{Store: st})
	assert.NilError(t, a.Tick(t.Context()))

	got, err := st.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Assert(t, !got.Archived, "task with future deadline must not be auto-archived yet")
}

func TestArchiver_Tick_BatchSizeBounded(t *testing.T) {
	// Not t.Parallel(): the archiver query is server-wide (no org filter), so
	// a batch picked while another test's tasks are also eligible would not
	// deterministically pick "this test's" tasks.
	st := teststore.New(t)
	org := teststore.CreateOrg(t, st, nil)

	const total = 5
	taskIDs := make([]int64, total)
	for i := 0; i < total; i++ {
		task := teststore.CreateTask(t, st, org, &teststore.TaskOptions{Status: model.TaskStatusCompleted})
		task.ArchiveAfter = -time.Hour
		assert.NilError(t, st.UpdateTask(t.Context(), nil, task))
		taskIDs[i] = task.ID
	}

	a := New(Options{Store: st, BatchSize: 2})
	assert.NilError(t, a.Tick(t.Context()))

	// Exactly batch-size tasks should be archived; the rest remain for later ticks.
	archived := 0
	for _, id := range taskIDs {
		got, err := st.GetTask(t.Context(), nil, id, org.OrgID)
		assert.NilError(t, err)
		if got.Archived {
			archived++
		}
	}
	assert.Equal(t, archived, 2, "expected batch-size limit to bound archives per tick")
}

func TestArchiver_AutoArchivedKind_Silent(t *testing.T) {
	// Not t.Parallel(): the archiver query is server-wide (no org filter),
	// so a parallel sibling could swallow our task in its own Tick (or vice
	// versa) and leave this test's publisher empty.
	//
	// Auto-archive must produce an audit log row but leave the channel
	// silent — admin housekeeping has nothing to say to the agent.
	st := teststore.New(t)
	org := teststore.CreateOrg(t, st, nil)

	task := teststore.CreateTask(t, st, org, &teststore.TaskOptions{Status: model.TaskStatusCompleted})
	task.ArchiveAfter = -time.Hour
	assert.NilError(t, st.UpdateTask(t.Context(), nil, task))

	pub := &pubsub.PublisherMock{
		PublishFunc: func(_ context.Context, _ model.Notification) error { return nil },
	}
	a := New(Options{Store: st, Publisher: pub})
	assert.NilError(t, a.Tick(t.Context()))

	logs, err := st.ListLogsByTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	var auto *model.Log
	for _, l := range logs {
		if strings.Contains(l.Content, "auto-archived") {
			auto = l
			break
		}
	}
	assert.Assert(t, auto != nil, "expected an auto-archived log row")

	// Find the publish for *our* task (if our Tick was the one that picked
	// it up); regardless of which archiver tick won, none of them should have
	// emitted a ChannelMessage for an auto-archive.
	for _, c := range pub.PublishCalls() {
		for _, r := range c.N.Resources {
			if r.Type == "task" && r.ID == task.ID {
				assert.Equal(t, c.N.ChannelMessage, "", "auto-archive must stay silent")
			}
		}
	}
}

func TestArchiver_TickRoundTripPersistsArchiveAfter(t *testing.T) {
	t.Parallel()
	st := teststore.New(t)
	org := teststore.CreateOrg(t, st, nil)

	d := 42 * time.Minute
	task := teststore.CreateTask(t, st, org, nil)
	task.ArchiveAfter = d
	assert.NilError(t, st.UpdateTask(t.Context(), nil, task))

	got, err := st.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, got.ArchiveAfter, d)
}
