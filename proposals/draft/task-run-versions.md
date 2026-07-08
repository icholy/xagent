# Task Version as Run Identity (Run-Versions)

Issue: https://github.com/icholy/xagent/issues/1274

## Problem

`tasks.version` is bumped by the task state machine but consumed by nothing:
every producer of runner events hardcodes `Version: 0`, which bypasses the
guard in `ApplyRunnerEvent` (`internal/model/task.go`). The version's one
convention — "0 overrides anything" — is actively harmful: a version-0 `failed`
from a dead run can clobber a live one (#1052), and 0 being the Go zero value
means *forgetting* to set a version silently grants bypass.

Meanwhile the system has no notion of a "run", even though a task's life is a
sequence of sandbox runs:

- **Driver logs are unattributable.** Restarts reuse the `xagent-{task-id}`
  container, so `docker logs` concatenates every run; there is no per-run key
  to store or display driver output against, and non-Docker backends have no
  `docker logs` at all.
- **The timeline can't be grouped by run.** Reports and lifecycle events from
  run 3 look exactly like those from run 1, and the started/exited lifecycle
  pairing that implies run boundaries can be missing entirely (#1052).
- **Event delivery to agents is unsynchronized.** A woken agent is told "the
  task was updated — check `get_my_task`", but the brief returns all events
  with no marker of which are new. The agent either re-handles old events or
  ignores the new one — the event is silently dropped. Server-side, there is
  no answer to "which run was this event assigned to, and did that run start?"

This proposal gives the version a single normative meaning — **the version is
the run counter** — and uses it to stamp events with run identity, fixing both
attribution (driver logs, timeline grouping) and agent event sync (dropped
events).

## Design

### 1. Version = run counter

`Task.Version` counts provisioned runs:

- **`0` — never run.** The task exists but no run has ever been queued.
- **`N ≥ 1` — run N is the latest provisioned run.** The version bumps to N at
  the moment run N is queued, and run N is the sandbox run that consumes that
  provisioning command.

The bump sites realign to match (this adopts §4 of
[`run-scoped-runner-events.md`](run-scoped-runner-events.md) verbatim):

- `Task.Start()` and `Task.Restart()` keep their `t.Version++` — each
  provisions a new run.
- `Task.Cancel()` **drops** its bump — stopping the current run is not a new
  run, and the live driver's terminal event (scoped to run N) must still
  apply, or cancellation wedges in `Cancelling`.
- The zombie-kill paths in `applyRunnerEventStarted` (archived/cancelled →
  `Cancelling+stop`) **drop** their bumps for the same reason: the zombie's
  `stopped` must land the task in `Cancelled`.

`CreateTask` (`internal/server/apiserver/task.go`) and the eventrouter's
rule-created tasks already seed `Version: 1` with `Pending+Start` — under the
new semantics that reads as "creation queues run 1", which is exactly what
happens. Version 0 therefore has no producer today; it is the reserved value
for any future create-without-start flow (drafts, templated tasks), and for
free it makes "has this task ever run?" a column predicate.

### 2. The bypass sentinel becomes -1

`RunnerEvent.Version` (proto and model) gets defined semantics:

- **`N ≥ 1`** — scoped to run N: applies only when `N == task.Version`.
- **`-1`** — unscoped bypass, the explicit replacement for today's
  "0 overrides anything". Reserved for emitters that genuinely cannot know the
  run (legacy `taskstate` records without a stamped version).
- **`0`** — invalid. No run 0 exists, so a 0-versioned event can never match;
  it is rejected as stale.

The guard in `ApplyRunnerEvent` becomes:

```go
// -1 bypasses the run check (unscoped events from legacy senders).
if e.Version != VersionBypass && e.Version != t.Version {
    return false // RunnerEventRejectedStale under run-scoped-runner-events
}
```

The zero-value hazard inverts from silent to loud: an emitter that forgets to
set `Version` now produces a rejected event (and, once run-scoped-runner-events
§3 lands, a visible "ignored" timeline entry) instead of silently clobbering
whatever run is current.

**Migration.** The server deploys before runners, and old runners/drivers send
0 meaning "bypass". Rejecting 0 immediately would strand their backstop
`failed` events and leave dead tasks stuck in `Running`. So the flip is
two-phase:

1. Server release A: accept both `0` and `-1` as bypass; all in-tree emitters
   (driver, runner backstops) switch to `-1`. The runner binary embeds the
   prebuilt driver, so both roll together.
2. Server release B (after runners are upgraded): `0` becomes reject-as-stale.

`taskstate.Record.Version == 0` (records written by older runners) is mapped to
`-1` at emit time — "legacy record, run unknown" — which is honest and keeps
the boot-time backstop working across the upgrade.

### 3. Events are stamped with their run: `events.run_version`

```sql
-- migration
ALTER TABLE events ADD COLUMN run_version bigint NOT NULL DEFAULT 0;
```

`0` means unattributed: pre-migration rows, and events on a task that has
never run. `model.Event` and `CreateEvent` (`internal/store/event.go`) gain the
field; the `Event` proto gains `int64 run_version`.

**Stamping rule: `run_version` = `task.Version` at commit time, where
wake-driven creation stamps *after* the `Start()` fold.** A wake event is
thereby assigned to the run it provisions. Per call site:

| Site | Stamp |
|---|---|
| `eventrouter.attach` wake path | post-`Start()` version — the run the event provisions (or the already-queued run when `Start()` refuses, e.g. `Pending`) |
| `eventrouter.attach` no-wake path | current version (needs a task read this path doesn't do today) |
| `UpdateTask` `AddInstructions` | post-`Start()` version when `req.Start` is set — requires folding `task.Start()` *before* the instruction-event creates (today the events are created first) |
| `CreateTask` / router create-task seeds | `1` — creation queues run 1 |
| `SubmitRunnerEvents` lifecycle events | the runner event's version when `≥ 1`, else the task's current version |
| `UploadLogs` report events | the loaded task row's version (the handler already reads it) |
| `CreateLink` link events, archiver / shell / update lifecycle events | current version |

The reorderings are small: `attach` locks the task row first (it already does
`GetTaskForUpdate` in the wake path), folds `Start()`, then creates the event;
`UpdateTask` moves the `req.Start` fold above the instruction loop.

**Delivery invariant.** A wake event stamped V commits in the same transaction
as (or after) the command that provisions run V, and run V's driver starts
strictly later — so *every wake event stamped V is visible to run V's first
`get_my_task`*. Events can no longer fall between runs:

- Task `Running` at N, event arrives → `Start()` bumps to N+1, event stamped
  N+1, `command=start` survives until run N+1's `started` consumes it. Even if
  run N's agent never polls again, run N+1 is guaranteed and owns the event.
- Task `Pending`/`Restarting` at N (run N queued, not started) → `Start()`
  refuses, event stamped N, run N starts later and sees it.
- Task terminal at N → `Start()` moves it to `Pending`, bumps to N+1, event
  stamped N+1.

The one remaining gap — an event attached while the task is `Cancelling` never
wakes anything — becomes *detectable* instead of invisible: a wake event
stamped V with no `SANDBOX_STARTED` lifecycle event stamped ≥ V on a terminal,
command-less task is a dropped delivery, queryable by a future reconciler (see
Open Questions).

### 4. Consumer: agent event sync

`GetTaskDetails` already returns the task (with `version`) and its events; with
`run_version` on the wire, the agent-side `get_my_task`
(`internal/agentmcp/xmcp.go`) can mark exactly which events are new:

- Each event in the brief carries its `run_version`.
- The brief gains a `new_events` marker: events with
  `run_version == task.version` are the ones assigned to the current run —
  the events this run was started *for*.

The bootstrap prompt's re-run branch (`internal/agent/PROMPT.md`, "The task
was updated. Check xagent:get_my_task and continue.") can then point the agent
at precisely the new events instead of asking it to diff the stream by
intuition. That closes both failure modes from the issue: the agent no longer
re-handles events a previous run already answered, and it can no longer
mistake a genuinely new event for an old one and drop it.

### 5. Consumer: driver logs

The driver learns its run version the same way run-scoped-runner-events §6
prescribes: `GetTask` at startup (it already calls this for the shell-session
fork). Baking the version into the sandbox spec is not an option — the Docker
adopt path (`internal/runner/backend/docker/docker.go`) reuses containers
without refreshing `Cmd`/`Env`/`Files`, so anything baked at first launch is
stale for every subsequent run of the same container.

What the run key enables (deliberately left as follow-ups — this proposal
delivers the key, not the features):

- **Per-run timeline grouping.** The web UI and `get_task` can render a task
  as a sequence of runs — run N's header from its `SANDBOX_STARTED`/`EXITED`
  lifecycle events, with the reports and external events stamped N nested
  under it — instead of one undifferentiated stream.
- **Per-run driver log capture.** A future slice can upload the driver's own
  output keyed by `(task_id, run_version)` (as a new event arm or a blob
  store), giving `xagent logs --run N` and UI access to exactly one run's
  output on any backend, instead of `docker logs`' concatenation on Docker
  only.

Reports uploaded through `UploadLogs` are stamped server-side from the task
row rather than by the driver. A report from a stale run that arrives after a
newer run was provisioned gets stamped with the newer version — a small,
bounded misattribution (see Trade-offs) that avoids threading the version
through the agent-mcp process.

### Relationship to `run-scoped-runner-events.md`

The two proposals share the version-realignment foundation and divide the rest:

- **This proposal** owns the version *semantics* (0 = never run, bump = new
  run), the `-1` bypass sentinel, and event run-stamping with its two
  consumers.
- **run-scoped-runner-events** owns the rejection machinery: the
  `ApplyRunnerEvent` verdict, per-event results on `SubmitRunnerEventsResponse`,
  ignored-event timeline entries, driver reaction to rejection, and the
  probe-driven start branch.

One amendment to that draft: wherever it says legacy emitters "degrade to
today's bypass behavior" with version 0, the bypass value becomes `-1` (with
the two-phase acceptance of 0 above). Its layers (4)–(6) and this proposal's
layers (1) and (6) below are the shared pieces; whichever lands first, the
other rebases mechanically.

## Implementation Plan

1. **Model: version realignment + `-1` sentinel** — Delivers:
   `model.VersionBypass = -1`; `Cancel` and the `applyRunnerEventStarted`
   zombie paths stop bumping; the guard accepts `-1` (and, behind a
   transition constant, `0`) as bypass and documents `0 = invalid`. Depends
   on: nothing (inert while all senders emit 0/bypass). Verifiable by: state
   machine tests — versioned cancel round-trip (cancel → `stopped` at the
   pre-cancel version → `Cancelled`); `0` and `-1` both bypass; `N != version`
   rejected.
2. **Migration + store: `events.run_version`** — Delivers: the column,
   sqlc regeneration, `model.Event.RunVersion`, `CreateEvent` writing it,
   list/get reads returning it. Depends on: nothing. Verifiable by: migration
   runs cleanly up and down; store round-trip test.
3. **Server: stamp every event-create site** — Delivers: the stamping table
   above, including the two reorderings (`attach` locks-then-folds-then-creates;
   `UpdateTask` folds `Start()` before the instruction loop) and the task read
   on the no-wake attach path. Depends on: (2). Verifiable by: handler and
   eventrouter tests asserting the stamped version per scenario (running task
   → N+1, pending task → N, terminal task → N+1, no-wake → N).
4. **Proto + agent brief: expose run versions** — Delivers: `run_version` on
   the `Event` message, the current-run marker in the agent-side `get_my_task`
   brief, and the PROMPT.md re-run wording pointing at new-this-run events.
   Depends on: (2), (3). Verifiable by: agentmcp brief test — events stamped
   with the current version are marked new, older ones are not.
5. **Runner + driver: emit `-1`** — Delivers: driver events and the runner's
   `supervise`/`failIfTaskRunning` backstops send `-1` instead of 0 (or the
   `taskstate` record's stamped version, once run-scoped-runner-events' layer
   lands); `Record.Version == 0` maps to `-1` at emit. Depends on: (1) deployed
   server-side. Verifiable by: runner/driver tests asserting the emitted
   version.
6. **Server: reject version 0** — Delivers: the transition constant flips;
   `0` is rejected as stale. Depends on: (5) rolled out to all runners.
   Verifiable by: state machine test; staged after a release gap.
7. **Web UI: per-run timeline grouping** *(optional follow-up slice)* —
   Delivers: the task timeline grouped by `run_version`, unattributed (0) rows
   in a single legacy group. Depends on: (4). Verifiable by: rendering a task
   with multiple runs.

Layers 1–4 are server-only and safe to merge independently; layer 5 rides the
normal runner+driver release; layer 6 is a one-line flip gated on rollout.

## Trade-offs

- **A dedicated `runs` table (or run UUID) instead of overloading `version`.**
  Cleaner identity semantics and a home for per-run metadata (started_at,
  exit reason), but it costs a new table, spec plumbing through both backends,
  and a parallel guard in the state machine. After the realignment, `version`
  already changes at exactly the run boundaries, so it serves as run identity
  with zero new plumbing — and a `runs` table can be added later keyed by
  `(task_id, run_version)` without unwinding anything here.
- **Keeping 0 as the bypass sentinel.** Rejected: 0 is now a meaningful state
  ("never run"), and the zero-value-means-bypass footgun is one of the root
  causes in #1052. `-1` must be set deliberately.
- **Driver-provided `run_version` on `UploadLogs` instead of server-side
  stamping.** More precise for the stale-run-report edge, but it threads the
  version through the agent-mcp process (a separate process from the driver)
  for a misattribution window that is rare and bounded to one version. Server-
  side stamping needs no wire or driver changes; revisit if misattribution
  shows up in practice.
- **Baking the run version into the sandbox spec / task token.** Rejected for
  the same reason run-scoped-runner-events rejected the `--version` flag: the
  Docker adopt path reuses containers without refreshing `Cmd`/`Env`/`Files`,
  so anything baked at first launch is permanently stale for reused
  containers. `GetTask` at driver startup works uniformly across backends and
  fresh/adopted sandboxes.
- **Stamping `run_version` lazily (view over lifecycle events) instead of a
  column.** Reconstructing run boundaries from started/exited pairs is exactly
  the heuristic that breaks today when those events go missing (#1052). The
  column is assigned transactionally with the provisioning decision, which is
  the whole point.

## Open Questions

- **Should a reconciler actively repair detectably-dropped wake events** (the
  `Cancelling`-attach case, or desync leftovers), e.g. re-queue tasks that
  have a wake event stamped newer than their last started run? Or is
  visibility (a UI badge / metric) enough to start?
- **Should the brief hide or de-emphasize events from completed runs** rather
  than just marking new ones? Hiding risks removing context the agent needs;
  marking is the conservative first step.
- **Does `get_my_task`'s `new_events` marker interact with mid-run polling?**
  An agent that polls mid-run sees events stamped N+1 (assigned to the next
  run) before that run starts. Proposed: still mark them new — handling early
  is fine, and run N+1 will see them again with full context.
