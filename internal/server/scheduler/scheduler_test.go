package scheduler

import (
	"sync"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// TestScheduler_Tick_FiresDueSchedule is the scheduler's happy path: a due
// schedule produces exactly one task, seeded identically to a hand-created one
// (a created event attributed to ScheduleActor, then one instruction event per
// template instruction), and its next_run_at is advanced past now.
func TestScheduler_Tick_FiresDueSchedule(t *testing.T) {
	// ClaimDueSchedules is server-wide (no org filter), so a tick claims every due
	// schedule in the database. Take the advisory lock guarding the due set so this
	// test's schedules are the only due rows while it runs — no t.Parallel(), and no
	// other package's due rows (e.g. the store's concurrent-claim test) get fired.
	teststore.LockScheduleDueSet(t)
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	ctx := t.Context()

	// A schedule whose next_run_at is already in the past, so this tick fires it.
	before := time.Now()
	past := before.Add(-time.Hour)
	sched := &model.Schedule{
		OrgID:     org.OrgID,
		CreatedBy: org.UserID,
		Name:      "nightly",
		Workspace: "w",
		Runner:    "r",
		Instructions: []model.ScheduleInstruction{
			{Text: "bump deps", URL: "https://example.com/deps"},
			{Text: "groom changelog"},
		},
		CronExpr:  "0 9 * * *",
		Timezone:  "UTC",
		Enabled:   true,
		NextRunAt: &past,
	}
	assert.NilError(t, s.CreateSchedule(ctx, nil, sched))

	sc := New(Options{Store: s})
	assert.NilError(t, sc.Tick(ctx))

	// Exactly one task materialized in this org.
	page, err := s.ListTasksPage(ctx, nil, store.ListTasksPageParams{OrgID: org.OrgID, PageSize: 100})
	assert.NilError(t, err)
	assert.Equal(t, len(page.Items), 1)

	// The schedule advanced: it points at the new task and its next fire is in the
	// future, so it is no longer due.
	got, err := s.GetSchedule(ctx, nil, sched.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Assert(t, got.LastTaskID != nil, "last_task_id must be set")
	assert.Assert(t, got.LastRunAt != nil, "last_run_at must be set")
	assert.Assert(t, got.NextRunAt != nil, "next_run_at must be set")
	assert.Assert(t, got.NextRunAt.After(before), "next_run_at must be advanced past now")

	// The task is a normal pending/start task, indistinguishable from a manual one.
	task, err := s.GetTask(ctx, nil, *got.LastTaskID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, task.Status, model.TaskStatusPending)
	assert.Equal(t, task.Command, model.TaskCommandStart)
	assert.Equal(t, task.Workspace, "w")
	assert.Equal(t, task.Runner, "r")

	// The event stream is created-by-ScheduleActor followed by one instruction
	// event per template instruction, in that order.
	events, err := s.ListEventsByTask(ctx, nil, task.ID, org.OrgID, nil)
	assert.NilError(t, err)
	assert.Assert(t, cmp.Len(events, 3))

	created, ok := events[0].Payload.(*model.LifecyclePayload)
	assert.Assert(t, ok, "first event must be a lifecycle event")
	assert.Equal(t, created.Kind, model.LifecycleKindCreated)
	assert.Equal(t, created.Actor, model.ScheduleActor)

	inst1, ok := events[1].Payload.(*model.InstructionPayload)
	assert.Assert(t, ok, "second event must be an instruction event")
	assert.Equal(t, inst1.Text, "bump deps")
	assert.Equal(t, inst1.URL, "https://example.com/deps")
	assert.Assert(t, events[1].Wake, "instruction events must wake")

	inst2, ok := events[2].Payload.(*model.InstructionPayload)
	assert.Assert(t, ok, "third event must be an instruction event")
	assert.Equal(t, inst2.Text, "groom changelog")
}

// TestScheduler_Tick_SkipsMissedOccurrences proves skip-only semantics: a
// schedule that was due many times over a simulated downtime fires exactly once
// on recovery and realigns to the grid, never backfilling the missed runs.
func TestScheduler_Tick_SkipsMissedOccurrences(t *testing.T) {
	// ClaimDueSchedules is server-wide (no org filter), so a tick claims every due
	// schedule in the database. Take the advisory lock guarding the due set so this
	// test's schedules are the only due rows while it runs — no t.Parallel(), and no
	// other package's due rows (e.g. the store's concurrent-claim test) get fired.
	teststore.LockScheduleDueSet(t)
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	ctx := t.Context()

	// A daily schedule whose next fire was three days ago: three occurrences
	// elapsed while the scheduler was "down".
	before := time.Now()
	missed := before.Add(-72 * time.Hour)
	sched := &model.Schedule{
		OrgID:     org.OrgID,
		CreatedBy: org.UserID,
		Name:      "nightly",
		Workspace: "w",
		Runner:    "r",
		CronExpr:  "0 9 * * *",
		Timezone:  "UTC",
		Enabled:   true,
		NextRunAt: &missed,
	}
	assert.NilError(t, s.CreateSchedule(ctx, nil, sched))

	sc := New(Options{Store: s})
	assert.NilError(t, sc.Tick(ctx))

	// The three missed occurrences collapse into a single fire.
	page, err := s.ListTasksPage(ctx, nil, store.ListTasksPageParams{OrgID: org.OrgID, PageSize: 100})
	assert.NilError(t, err)
	assert.Equal(t, len(page.Items), 1)

	// next_run_at realigned to the first occurrence strictly after now, not to the
	// occurrence after the stale next_run_at (which would still be in the past).
	got, err := s.GetSchedule(ctx, nil, sched.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Assert(t, got.NextRunAt != nil)
	assert.Assert(t, got.NextRunAt.After(before), "next_run_at must realign to the future")

	// A second immediate tick finds nothing due and creates no further task.
	assert.NilError(t, sc.Tick(ctx))
	page, err = s.ListTasksPage(ctx, nil, store.ListTasksPageParams{OrgID: org.OrgID, PageSize: 100})
	assert.NilError(t, err)
	assert.Equal(t, len(page.Items), 1)
}

// TestScheduler_Tick_ConcurrentFiresOnce proves the FOR UPDATE SKIP LOCKED
// guarantee end-to-end: with two schedulers ticking at the same instant, a due
// schedule fires exactly once — one tick claims and fires it, the other skips the
// locked row (and, once the fire commits, sees it is no longer due).
func TestScheduler_Tick_ConcurrentFiresOnce(t *testing.T) {
	// ClaimDueSchedules is server-wide (no org filter), so a tick claims every due
	// schedule in the database. Take the advisory lock guarding the due set so this
	// test's schedules are the only due rows while it runs — no t.Parallel(), and no
	// other package's due rows (e.g. the store's concurrent-claim test) get fired.
	teststore.LockScheduleDueSet(t)
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	ctx := t.Context()

	past := time.Now().Add(-time.Hour)
	assert.NilError(t, s.CreateSchedule(ctx, nil, &model.Schedule{
		OrgID:     org.OrgID,
		CreatedBy: org.UserID,
		Name:      "nightly",
		Workspace: "w",
		Runner:    "r",
		CronExpr:  "* * * * *",
		Timezone:  "UTC",
		Enabled:   true,
		NextRunAt: &past,
	}))

	sc := New(Options{Store: s})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = sc.Tick(ctx)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		assert.NilError(t, err)
	}

	// Exactly one task despite two concurrent ticks.
	page, err := s.ListTasksPage(ctx, nil, store.ListTasksPageParams{OrgID: org.OrgID, PageSize: 100})
	assert.NilError(t, err)
	assert.Equal(t, len(page.Items), 1)
}

// TestScheduler_Tick_DisablesInvalidSchedule covers the invalid-at-fire-time edge
// case: a schedule whose timezone can no longer be resolved is disabled (enabled
// = false, next_run_at = NULL) rather than silently wedging as permanently due.
// The schedule is seeded through the store directly (bypassing the API's
// create-time Validate) to reproduce a tz that has since disappeared from the tz
// database.
func TestScheduler_Tick_DisablesInvalidSchedule(t *testing.T) {
	// ClaimDueSchedules is server-wide (no org filter), so a tick claims every due
	// schedule in the database. Take the advisory lock guarding the due set so this
	// test's schedules are the only due rows while it runs — no t.Parallel(), and no
	// other package's due rows (e.g. the store's concurrent-claim test) get fired.
	teststore.LockScheduleDueSet(t)
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	ctx := t.Context()

	past := time.Now().Add(-time.Hour)
	sched := &model.Schedule{
		OrgID:     org.OrgID,
		CreatedBy: org.UserID,
		Name:      "nightly",
		Workspace: "w",
		Runner:    "r",
		CronExpr:  "0 9 * * *",
		Timezone:  "Nowhere/Bad",
		Enabled:   true,
		NextRunAt: &past,
	}
	assert.NilError(t, s.CreateSchedule(ctx, nil, sched))

	sc := New(Options{Store: s})
	assert.NilError(t, sc.Tick(ctx))

	// The due occurrence still fired once; the schedule is then disabled so it
	// leaves the claim query instead of firing forever.
	got, err := s.GetSchedule(ctx, nil, sched.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Assert(t, !got.Enabled, "an unresolvable schedule must be disabled")
	assert.Assert(t, got.NextRunAt == nil, "a disabled schedule must have a NULL next_run_at")
	assert.Assert(t, got.LastTaskID != nil, "the fire that triggered the disable is still recorded")

	// The row stays out of the due set on subsequent ticks: no further task, so
	// the recorded last-run task is unchanged.
	assert.NilError(t, sc.Tick(ctx))
	page, err := s.ListTasksPage(ctx, nil, store.ListTasksPageParams{OrgID: org.OrgID, PageSize: 100})
	assert.NilError(t, err)
	assert.Equal(t, len(page.Items), 1)
	got2, err := s.GetSchedule(ctx, nil, sched.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Assert(t, got2.LastTaskID != nil)
	assert.Equal(t, *got2.LastTaskID, *got.LastTaskID)
}
