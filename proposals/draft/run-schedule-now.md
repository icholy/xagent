# Run a Schedule Now (Manual Trigger)

Issue: https://github.com/icholy/xagent/issues/1481

## Problem

Schedules fire on their cron cadence and nothing else. After writing a schedule you have
no way to see it actually run — you either wait for its next occurrence or hand-copy the
template into a one-off `CreateTask`. That's the wrong shape for the two things people do
right after creating a schedule: testing that the instructions/workspace/runner are correct,
and kicking the work off ad-hoc when it's needed before the next tick.

We want a **"Run now"** action on the Schedules page that fires a schedule immediately,
producing a task identical to what the next scheduled occurrence would have produced. The
manual run is an *extra* one-off fire: it must not disturb the cron cadence — the next
scheduled run stays exactly where it was.

## Design

### Overview

The scheduler worker already turns a due schedule into a real task through a single method,
`Scheduler.fire` (`internal/server/scheduler/scheduler.go`). That method does two separable
things:

1. **Materialize one occurrence** — insert the template task (`sched.Task()`), seed its
   events (`sched.Events(task)`: a `LifecycleKindCreated` event attributed to
   `model.ScheduleActor` plus one wake-carrying `InstructionPayload` event per instruction),
   and build the task-created change notification (`{created task, appended task_events}` with
   `Runner: task.PendingRunner()` so the runner wakes).
2. **Advance the cadence** — recompute `Next(now)` and write `next_run_at`, `last_run_at`,
   `last_task_id` via `AdvanceSchedule`.

A manual run is exactly step 1 with a *different* step 2: it records that the schedule ran
(`last_run_at`, `last_task_id`) but leaves `next_run_at` untouched. So the design is a small
refactor — split the occurrence out of `fire` into a shared function — plus a new RPC and a UI
button that call it.

The key property we want to guarantee structurally, not by convention, is **"a manual run and
a scheduled run produce an identical task."** The only way to keep that true as the code
evolves is to have both paths call the *same* occurrence-materializing function, rather than
re-deriving the task/events in the handler.

### The shared fire path

Extract the occurrence half of `fire` into an exported function in the `scheduler` package
that takes the store subset it needs, so both the worker and the API handler call it:

```go
// internal/server/scheduler/scheduler.go

// Fire materializes one schedule occurrence inside tx: it creates the template
// task and seeds its events exactly as CreateTask does, and returns the task plus
// the change notification to publish once tx commits. It does NOT advance the
// schedule — the caller decides whether this fire moves the cron cadence
// (Scheduler.fire, via AdvanceSchedule) or is a one-off manual run (RunSchedule,
// which records only last_run_at/last_task_id). Sharing this function is what
// makes a manual run and a scheduled run produce a byte-for-byte identical task.
func Fire(ctx context.Context, tx *sql.Tx, st TaskStore, sched *model.Schedule) (*model.Task, model.Notification, error) {
    task := sched.Task()
    if err := st.CreateTask(ctx, tx, task); err != nil {
        return nil, model.Notification{}, err
    }
    for _, ev := range sched.Events(task) {
        if err := st.CreateEvent(ctx, tx, ev); err != nil {
            return nil, model.Notification{}, err
        }
    }
    return task, model.Notification{
        Type: "change",
        Resources: []model.NotificationResource{
            {Action: "created", Type: "task", ID: task.ID},
            {Action: "appended", Type: "task_events", ID: task.ID},
        },
        OrgID:          task.OrgID,
        Runner:         task.PendingRunner(),
        Time:           time.Now(),
        ChannelMessage: fmt.Sprintf("Task %d created on %s/%s.", task.ID, task.Runner, task.Workspace),
    }, nil
}
```

`TaskStore` is the two-method interface `Fire` needs (`CreateTask`, `CreateEvent`) — a subset
of the existing scheduler `Store`, satisfied by `*store.Store` unchanged. `Scheduler.fire`
becomes: call `Fire`, then `AdvanceSchedule` (the cadence half is unchanged).

The template → task/events mapping is *already* shared: it lives in `model.Schedule.Task()`
and `model.Schedule.Events()`. This refactor pulls the remaining duplicated piece — the store
writes and the task-created notification literal — up into one function too, so the manual and
scheduled paths can't drift.

### API surface

One new RPC on `XAgentService`, placed alongside the existing Schedule RPCs in
`proto/xagent/v1/xagent.proto`:

```proto
rpc CreateSchedule(CreateScheduleRequest) returns (CreateScheduleResponse);
rpc GetSchedule(GetScheduleRequest) returns (GetScheduleResponse);
rpc ListSchedules(ListSchedulesRequest) returns (ListSchedulesResponse);
rpc UpdateSchedule(UpdateScheduleRequest) returns (UpdateScheduleResponse);
rpc DeleteSchedule(DeleteScheduleRequest) returns (DeleteScheduleResponse);
rpc SetScheduleEnabled(SetScheduleEnabledRequest) returns (SetScheduleEnabledResponse);
rpc RunSchedule(RunScheduleRequest) returns (RunScheduleResponse);   // new
```

```proto
// RunScheduleRequest fires a schedule immediately as a one-off, in addition to
// its cron cadence. The next scheduled occurrence (next_run_at) is left untouched;
// only last_run_at/last_task_id are updated to point at the run this created.
// Allowed on a disabled schedule — a disabled schedule only means "don't fire
// automatically," and testing a not-yet-enabled schedule is the primary use case.
message RunScheduleRequest {
  int64 id = 1;
}

message RunScheduleResponse {
  Task task = 1;          // the task the manual run created — for the UI to link to
  Schedule schedule = 2;  // refreshed last_run_at/last_task_id; next_run_at unchanged
}
```

Returning both lets the UI navigate straight to the new run *and* refresh the schedule row's
"Last run: #NNNN" link in one round trip.

### Handler

New handler in the existing `internal/server/apiserver/schedule.go`:

```go
func (s *Server) RunSchedule(ctx context.Context, req *xagentv1.RunScheduleRequest) (*xagentv1.RunScheduleResponse, error) {
    caller := apiauth.MustCaller(ctx)
    var (
        task  *model.Task
        note  model.Notification
        sched *model.Schedule
    )
    err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
        existing, err := s.store.GetScheduleForUpdate(ctx, tx, req.Id, caller.OrgID)
        if err != nil {
            return err
        }
        // A manual run materializes a real task on the schedule's target, so it
        // demands the same task-create scope CreateSchedule/UpdateSchedule require —
        // stronger than the task-write tier used for enable/disable. The target is
        // read from the row, not the request, so it can't be spoofed.
        if !caller.Scopes.Allow(authscope.OpTaskCreate,
            authscope.WithTaskWorkspace(existing.Workspace),
            authscope.WithTaskRunner(existing.Runner),
            authscope.WithTaskArchived(false),
        ) {
            return connect.NewError(connect.CodePermissionDenied, errors.New("cannot run schedule"))
        }

        // Same occurrence the scheduler worker fires — identical task and events.
        task, note, err = scheduler.Fire(ctx, tx, s.store, existing)
        if err != nil {
            return err
        }

        // Record the run WITHOUT advancing the cadence: next_run_at is left as-is,
        // so the next scheduled occurrence stays exactly where it was. This never
        // calls Next(), so a manual run works even on a disabled schedule (whose
        // next_run_at is NULL) or one whose cron/timezone later became invalid.
        firedAt := time.Now().UTC()
        if err := s.store.RecordScheduleRun(ctx, tx, existing.ID, caller.OrgID, firedAt, task.ID); err != nil {
            return err
        }
        existing.LastRunAt = &firedAt
        existing.LastTaskID = &task.ID
        sched = existing
        return tx.Commit()
    })
    if err != nil {
        if connect.CodeOf(err) != connect.CodeUnknown {
            return nil, err
        }
        if errors.Is(err, sql.ErrNoRows) {
            return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("schedule %d not found", req.Id))
        }
        return nil, connect.NewError(connect.CodeInternal, err)
    }
    s.log.InfoContext(ctx, "schedule run manually", "id", sched.ID, "task", task.ID)
    // Publish the same task-created notification the scheduler and CreateTask emit,
    // AFTER commit, so the runner wake channel and Web UI never see a rolled-back
    // task. Fire built it with Runner set; stamp in the acting user for the SSE fan-out.
    note.UserID, note.ClientID = caller.ID, caller.ClientID
    s.publish(note)
    return &xagentv1.RunScheduleResponse{Task: task.Proto(s.baseURL), Schedule: sched.Proto()}, nil
}
```

### Store

Add one focused query — the manual sibling of `AdvanceSchedule` that writes the run bookkeeping
but deliberately *omits* `next_run_at`:

```sql
-- name: RecordScheduleRun :exec
-- Record a manual ("run now") fire: update last_run_at/last_task_id but NOT
-- next_run_at, so the cron cadence is undisturbed. Contrast AdvanceSchedule,
-- which also moves next_run_at.
UPDATE schedules
SET last_run_at = $1, last_task_id = $2,
    updated_at = (NOW() AT TIME ZONE 'UTC')
WHERE id = $3 AND org_id = $4;
```

and the thin wrapper in `internal/store/schedule.go`:

```go
func (s *Store) RecordScheduleRun(ctx context.Context, tx *sql.Tx, id, orgID, taskID int64, ranAt time.Time) error
```

Keeping `next_run_at` out of the SQL — rather than reading the row and re-writing it via
`UpdateSchedule` — makes "a manual run never touches the cadence" an explicit property of the
query, not something that depends on faithfully round-tripping every column.

### Behavior decisions

The issue raises three behaviors; each is settled here.

- **The cron cadence must not advance (`next_run_at`).** This is the core requirement. The
  manual path never calls `Next()` and never writes `next_run_at`; `RecordScheduleRun` omits
  the column. A nightly `0 9 * * *` schedule that you "Run now" at 14:00 still fires next at
  09:00 tomorrow. The extra run is purely additive.

- **`last_task_id`/`last_run_at` *are* updated.** A manual run is a real run of the schedule,
  and the whole point of "Run now" is to test it and inspect the result. The Schedules list
  already surfaces `last_task_id` as a "Last run: #NNNN" link; after Run now that link should
  point at the run you just triggered. We define **"last run" as the most recent run of the
  schedule regardless of trigger**, not "last *scheduled* run." The trade-off: a manual run
  overwrites the pointer to the previous scheduled run. That's acceptable — "the run that just
  happened" is the more useful meaning, and the lifecycle event on the created task records that
  it was fired by `ScheduleActor` for anyone who needs provenance. (See Open Questions for the
  alternative of not touching the pointer.)

- **Allowed on a disabled schedule.** Yes. `enabled = false` means "don't fire *automatically*"
  — it takes the row out of the scheduler's claim query — but the template is still valid and a
  manual trigger is an explicit human action, not an automatic one. Allowing it directly serves
  the "test right after writing" flow: creates default to inert unless `enabled` is set true
  (`CreateScheduleRequest.enabled`), so the natural loop is *create disabled → Run now to test →
  enable*. Because the manual path never computes `Next()`, running a disabled schedule leaves
  `next_run_at` NULL and the schedule stays disabled — Run now never silently re-enables
  anything.

### Web UI

Add a **"Run now"** action to each row of the Schedules list
(`webui/src/routes/schedules.index.tsx`), in the existing action button group alongside
Edit/Delete, using the `Play` icon already imported elsewhere (`task-timeline.tsx`):

- A `useMutation(runSchedule)` call fired on click with `{ id: schedule.id }`. The button shows
  a spinner (`Loader2`) while pending, matching the delete flow.
- The button is **always enabled**, including for disabled schedules (that's the test-first
  use case), with `aria-label="Run schedule now"`.
- On success, navigate to the created task (`/tasks/$id` with the returned `task.id`) so the
  user lands on the run they just triggered — the same "watch it run" payoff the create form's
  post-submit navigation gives. The list's `last_task_id` link refreshes on the next 30s poll /
  the `schedule updated` SSE change event; no manual `refetch` needed, though `refetch()` on
  success keeps it instant.

There is no confirm dialog: Run now is a low-friction, deliberately-repeatable action (unlike
Delete, which is destructive). Run `pnpm lint` in `webui/` before finishing.

## Implementation Plan

A layer cake — each slice is small, independently reviewable, and safe to merge before the ones
above it land.

1. **Store method** — Delivers: the `RecordScheduleRun` query in
   `internal/store/sql/queries/schedule.sql`, its generated code (`mise run generate`), and the
   `*store.Store` wrapper in `internal/store/schedule.go`. Depends on: nothing (the `schedules`
   table already exists). Verifiable by: a store unit test asserting `RecordScheduleRun` updates
   `last_run_at`/`last_task_id` and leaves `next_run_at` unchanged.
2. **Refactor `fire` → shared `Fire`** — Delivers: the exported `scheduler.Fire` function + the
   `TaskStore` interface, with `Scheduler.fire` reduced to `Fire` + `AdvanceSchedule`. Depends
   on: nothing. Verifiable by: the existing scheduler `Tick` tests still pass unchanged (pure
   refactor — a due schedule still produces exactly one task, the right events, and an advanced
   `next_run_at`).
3. **Proto + RPC handler** — Delivers: `RunSchedule` RPC, `RunScheduleRequest`/`Response`
   messages, `mise run generate`, and the `RunSchedule` handler in
   `internal/server/apiserver/schedule.go` with `OpTaskCreate` authorization. Depends on: (1),
   (2). Verifiable by: handler tests asserting a manual run creates one task with the
   `ScheduleActor` created-event + instruction events, updates `last_task_id`, leaves
   `next_run_at` untouched, succeeds on a disabled schedule, and is denied for a caller without
   task-create scope on the target.
4. **Web UI** — Delivers: the "Run now" button on the Schedules list, wired to the `runSchedule`
   mutation with navigation to the created task. Depends on: (3). Verifiable by: rendering the
   list against seeded schedules, clicking Run now, landing on the new task; `pnpm lint` passes.

## Trade-offs

| Approach | Pros | Cons |
|----------|------|------|
| **Extract `Fire`, new `RunSchedule` RPC, `RecordScheduleRun` store method** (proposed) | Manual and scheduled runs share one occurrence function, so an identical task is guaranteed structurally; "don't touch the cadence" is explicit in a dedicated query; disabled-schedule and no-`Next()` behavior fall out naturally | Adds one RPC, one small query, and a refactor of `fire`; apiserver takes a dependency on the `scheduler` package for `Fire` |
| **Handler re-derives the task/events inline** (don't extract `Fire`) | No refactor of `fire`; no apiserver→scheduler import | Duplicates the template→task/events/notification wiring; the "identical task" guarantee becomes convention that silently rots the first time either path changes |
| **Reuse `AdvanceSchedule`, then recompute `next_run_at` back** | No new store method | Reads/rewrites the cadence only to restore it; racy and semantically backwards — the manual path shouldn't compute `Next()` at all |
| **Record the run via `UpdateSchedule` (full-row round-trip)** | Reuses an existing store method | "Never touches `next_run_at`" depends on faithfully round-tripping every column; rewrites instructions/cron/tz needlessly; less clear than a scoped query |
| **Overload an existing RPC (e.g. a `run_now` flag on `SetScheduleEnabled`)** | No new RPC | Conflates two unrelated actions (toggle vs. fire); muddies the auth tier (enable is task-write, a fire is task-create); awkward response shape |
| **Client-side "copy template into `CreateTask`"** (no backend) | Zero backend work | The manual run wouldn't be attributed to `ScheduleActor`, wouldn't update `last_task_id`, and would drift from the real fire path — the exact thing the issue's "identical task" intent rules out |

## Open Questions

1. **Does a manual run move the "Last run" pointer?** The design says yes — `last_task_id` points
   at the most recent run regardless of trigger. The alternative is to leave `last_task_id`
   pointing at the last *scheduled* run and surface the manual run only via the returned task +
   its `ScheduleActor` event (so the list's "Last run" always means "last automatic run"). That
   needs a way to show the manual run in the UI without the pointer, which the current single
   `last_task_id` column can't express — deferred unless the "last scheduled run" reading turns
   out to matter.
2. **Confirmation before firing.** Run now starts a real container run that costs resources. The
   proposal makes it a single-click action (low friction is the point). If accidental fires
   become a problem, a lightweight confirm dialog (like Delete's) can be added without touching
   the backend.
3. **Concurrent manual runs / overlap.** Like scheduled occurrences, nothing stops "Run now" from
   creating a run while a previous run (manual or scheduled) is still going — they run as
   independent tasks, consistent with the existing "overlapping runs are allowed" decision in the
   scheduled-tasks proposal. No overlap guard is proposed here.
