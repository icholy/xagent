# Restructure tasks as a unified event stream

Issue: https://github.com/icholy/xagent/issues/947

## Problem

A task's history is scattered across separate concepts with separate code
paths, storage, and wakeup behavior, and every consumer has to stitch them back
together:

- **Instructions** live as a JSON array in `tasks.instructions`
  (`internal/store/sql/schema.sql`), and "add instruction" mutates the column
  and sets `command = start` to trigger a restart
  (`apiserver/task.go` `UpdateTask`).
- **Logs** are their own table (`logs`: `id, task_id, type, content,
  created_at`) written through `UploadLogs` / `store.CreateLog`. The `type`
  column already overloads five distinct meanings: `llm` (the agent's `report`
  tool, `agentmcp/xmcp.go:133`), `mcp` (per-tool-call logs,
  `agentmcp/xmcp.go:43`), `audit` (task mutations, `apiserver/task.go`), `info`
  (container lifecycle, `apiserver/runner.go:105`), and `error`.
- **External events** are an org-scoped `events` table fanned out to tasks via
  the `event_tasks` join, routed by `eventrouter.Route` matching an event's
  `model.RoutingKey(url)` against subscribed `task_links`.
- **Lifecycle** changes arrive as `RunnerEvent`s (`started`/`stopped`/`failed`)
  through `SubmitRunnerEvents`, folded into `tasks.status` by
  `Task.ApplyRunnerEvent` (`internal/model/task.go`), and *separately* written
  as `audit`/`info` log rows.

The system is already half event-sourced. `proposals/accepted/driver-owned-events.md`
made the driver the source of truth for lifecycle events submitted to an API,
and `ApplyRunnerEvent` is literally a fold over those events into a status. Two
draft proposals are circling the same center of gravity from different sides:

- `proposals/draft/task-change-unifying-logs-and-notifications.md` proposes a
  `model.TaskChange` value type so a single structured fact projects to both an
  audit-log row and a channel notification.
- `proposals/draft/richer-tool-call-logs.md` enriches the `mcp` log channel.

This proposal unifies the four channels into one ordered, typed, per-task event
stream, and folds the `TaskChange` work in as the `lifecycle` event type.

## Design

### The stream: `events`

A single append-only table is the source of truth for everything that happens
to a task. Order is the stream; the `id` (a `BIGSERIAL`) is the ordering key.
It reuses the `events` name freed by dropping the old org-level `events` table
and its `event_tasks` join (see Rollout).

```sql
CREATE TABLE public.events (
    id         bigint  NOT NULL,                       -- BIGSERIAL; stream order
    task_id    bigint  NOT NULL,
    org_id     bigint  NOT NULL,
    type       text    NOT NULL,                       -- instruction|report|external|lifecycle|link
    wake       boolean NOT NULL DEFAULT false,         -- does appending this wake an idle task?
    payload    jsonb   NOT NULL,                        -- type-specific (see below)
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);
CREATE INDEX idx_events_task_id_id ON public.events (task_id, id);
CREATE INDEX idx_events_org_id     ON public.events (org_id);   -- powers the org event feed
```

The whole model reduces to two verbs:

- **append** an event (instruction added, report written, external event
  arrived, lifecycle transition, link created),
- **read** a task's events in order.

Reading a task's stream is one query — `WHERE task_id = $1 ORDER BY id`.
Clients fetch the whole stream each time, exactly as they fetch all
instructions, logs, and events today. Incremental delivery — reading only
what's new since a cursor — is a separate concern (#946) and out of scope here.

#### Event types, `wake`, and the derived direction

The issue's classifying axes — **type** and **wake** — become explicit columns
rather than implied by which table a row lives in. **Direction** (`to_agent` /
`from_agent` / `about_task`) is a convenient way to talk about the types, but it
is strictly a function of `type` (the mapping below), so it is *derived*, not
stored.

| `type`        | direction *(derived)* | `wake` (default) | Replaces today                                    |
| ------------- | --------------------- | ---------------- | ------------------------------------------------- |
| `instruction` | `to_agent`            | `true`           | `tasks.instructions` JSON + `command=start`       |
| `external`    | `to_agent`            | per routing rule | org-level `events` + `event_tasks` fan-out        |
| `report`      | `from_agent`          | `false`          | `logs` rows with `type='llm'` (the `report` tool) |
| `lifecycle`   | `about_task`          | `false`          | `RunnerEvent` fold + `audit`/`info` log rows      |
| `link`        | `about_task`          | `false`          | `task_links` row creation                         |

Because direction is a pure function of `type`, it is not a column — storing it
would only invite drift for no gain. Queries that want a direction filter spell
out the types instead: the brief is `type IN ('instruction','external')`, and
the UI timeline groups by the same mapping.
`wake`, by contrast, is *not* purely a function of type: instructions always wake, reports
never do, but an `external` event wakes only when its routing rule has
`wakeup = true` (`eventrouter.attach`, today's `rule.Wakeup`). So `wake` is set
per-event at append time. This is the unification the issue asks for: **"add
instruction → restart" and "event arrived → notify" become the same
mechanism** — append an event whose `wake` is true, which sets `command=start`
exactly as `Task.Start()` does today (`internal/model/task.go:416`).

#### Payloads

`payload jsonb` carries the type-specific fields. Shapes:

```jsonc
// instruction  — was model.Instruction + actor
{ "text": "rebase onto main", "url": "https://github.com/.../pull/481", "actor": {"kind":"user","name":"icholy"} }

// external     — self-contained; the org event feed reads these by org_id
{ "description": "PR comment from alice on #481", "url": "https://github.com/.../481#issuecomment-…", "data": "{…webhook…}" }

// report       — the agent's report tool (was logs type='llm')
{ "content": "Opened PR #952 with the migration." }

// lifecycle    — the model.TaskChange fact (see below)
{ "kind": "container_exited", "actor": {"kind":"runner"}, "from_status": "RUNNING", "to_status": "COMPLETED", "runner_event": "stopped" }

// link         — was task_links row
{ "link_id": 1027, "relevance": "trigger", "url": "https://github.com/.../issues/947", "title": "icholy commented…", "subscribe": true }
```

### `lifecycle` events absorb the `TaskChange` proposal

`proposals/draft/task-change-unifying-logs-and-notifications.md` already
designed the closed set of "things that happened to a task": `Created`,
`Updated`, `Cancelled`, `Restarted`, `Archived`, `Unarchived`, `AutoArchived`,
`Woken`, `ContainerStarted`, `ContainerExited`, `ContainerFailed`. Those are
exactly the `lifecycle` event `payload.kind` values here. The two proposals
converge:

- `TaskChange`'s "single structured fact" *is* a `lifecycle` event.
- Its `Log()` projection becomes the timeline rendering of the event (no more
  canned `container exited successfully`; the `to_status` in the payload says
  whether the container completed, re-queued, or was cancelled).
- Its `Notification()` / `ChannelMessage` projection is unchanged — the
  notification is published from the appended event after commit.
- `Woken` is no longer a `lifecycle` event at all: it splits cleanly into the
  `external` event (the trigger) plus whatever `lifecycle` transition the wake
  caused. The "which webhook woke me and what did it say" information the
  `TaskChange` proposal was recovering is now first-class in the `external`
  event's payload, not reconstructed into a log string.

`Task.ApplyRunnerEvent` keeps its exact state-machine logic. The only change:
`SubmitRunnerEvents` appends a `lifecycle` event **and** folds it into the
materialized `status` column in the same transaction, instead of writing an
`info`/`audit` log row beside the status mutation.

### Status stays a projection

The status fold is the canonical example of event-sourcing already living in
the codebase, and it stays. `tasks.status`, `tasks.command`, and
`tasks.version` remain materialized columns — `ListTasks` must not replay
streams to render a list, and the runner poll
(`ListTasksForRunner`: `WHERE command != 0`) must stay an indexed column scan.

The rule: **`lifecycle` events are the source of truth; `status` is a
projection updated in the same transaction by the existing fold.** The
status guard (`ApplyRunnerEvent`'s version check and per-status transition
rules, plus `Start`/`Restart`/`Cancel`) carries over unchanged — it is the fold
function. `command`/`version` remain the runner-coordination channel and are
*not* stream events (they are control state, not history).

### Links: event is truth, `task_links` is the index

`link` events are the source of truth for the timeline. But the subscription
matcher needs to answer "which tasks subscribe to `routing_key K`?" with an
index, not a stream scan. So `task_links` survives as a **projection**: when a
`link` event with `subscribe=true` is appended, the same transaction upserts a
row into `task_links` (`task_id, routing_key, subscribe`), keeping the existing
`idx_task_links_routing_key`. `eventrouter` continues to call
`FindSubscribedLinksForOrgs` against that projection unchanged. The UI reads
links from the projection (cheap); the timeline reads them from the stream.

### The agent's brief

The agent's brief — what a run is handed — is just the task's to-agent events,
the `instruction` and `external` types:

```sql
SELECT * FROM events
WHERE task_id = $1 AND type IN ('instruction', 'external')
ORDER BY id;
```

`report`, `lifecycle`, and `link` events are not to-agent, so they never
enter the brief — they are the task's own output and history, surfaced in the
UI timeline (#918) but not pushed back at the agent.

Clients fetch the brief in full on each run, exactly as they fetch all
instructions and events today. Incremental delivery — handing the agent only
the events new since its last run, so it doesn't re-process the whole stream —
is a separate concern (#946) and out of scope here.

### The agent contract

The contract reduces to "consume your events, append events as you work," and
the MCP tools (`internal/agentmcp/xmcp.go`) become thin verbs over
the stream:

| Tool                     | Today                                   | Under the stream                                          |
| ------------------------ | --------------------------------------- | --------------------------------------------------------- |
| `get_my_task`            | `GetTaskDetails` (4 joins)              | read this task's to-agent events (brief)                  |
| `report`                 | `UploadLogs` type=`llm`                 | append a `report` event                                   |
| `create_link`            | `CreateLink`                            | append a `link` event (projection upsert follows)         |
| `create_child_task`      | `CreateTask`                            | unchanged; child gets its own stream                      |
| `update_child_task`      | `UpdateTask(start=true)`                | append an `instruction` event to the **child's** stream   |
| `list_child_task_logs`   | `ListLogs`                              | read the child's stream                                   |

"Child interaction" is "append to / read the child's stream," with the same
token-scoped authorization that exists today (the task token's `task.write`
scope, plus own-or-child matching via `Task.ScopeAttr`).

### Raw output vs. semantic events

The issue flags the volume problem: `report`s are semantic and low-volume;
per-tool-call `mcp` logs and any future raw stdout/transcript streaming are
orders of magnitude higher. Putting megabytes of transcript chunks into
`events` would bloat the stream that the brief renderer and timeline scan.

**Recommendation: keep a separate verbose channel.** `events` holds the
five semantic types. The existing `logs` table is retained but narrowed to the
**verbose channel** — `mcp` tool-call logs (the subject of
`proposals/draft/richer-tool-call-logs.md`) and any future raw transcript. The
semantic log types migrate out:

- `llm`  → `report` events
- `audit`→ `lifecycle` events
- `info` → `lifecycle` events
- `mcp`  → stays in `logs` (verbose channel)
- `error`→ stays in `logs`, or rides as a `lifecycle` payload field for
  container failures

The brief renderer never touches `logs`. The #918 timeline does a cheap
ordered union of `events` (always shown) and `logs` (collapsible /
filterable "noise" tier), which is exactly the density/filtering control that
issue asks for. The alternative — one table with a `volume` discriminator — is
discussed under Trade-offs.

### API / proto changes

`proto/xagent/v1/xagent.proto` redefines `Event` as the stream row and adds
stream-shaped RPCs. The old org-level event RPCs and the per-channel log/fan-out
RPCs collapse into them: `ListEvents` is re-pointed at the stream (it can scope
by `org_id` for the feed or `task_id` for a timeline), and `GetEvent`,
`CreateEvent`, `UploadLogs`, `ListLogs`, `ListEventsByTask`,
`AddEventTask`/`RemoveEventTask` go away.

```proto
message Event {
  int64 id = 1;
  int64 task_id = 2;
  string type = 3;       // instruction|report|external|lifecycle|link
  bool wake = 4;
  google.protobuf.Struct payload = 5;
  google.protobuf.Timestamp created_at = 6;
}

// Append one event. report = AppendEvent(type=report);
// adding an instruction to a child = AppendEvent(type=instruction) on the child.
rpc AppendEvent(AppendEventRequest) returns (AppendEventResponse);

// Read the stream. Scoped by task_id (a task's timeline / the brief, the latter
// filtered to type in instruction, external) or by org_id (the org event feed,
// filtered to type=external). Optional type filter.
rpc ListEvents(ListEventsRequest) returns (ListEventsResponse);
```

`SubmitRunnerEvents` is unchanged on the wire (the runner still submits
`started`/`stopped`/`failed`); the handler additionally appends a `lifecycle`
event. `CreateLink` and `CreateTask` keep their RPCs (ergonomic, validated
entry points) but their handlers now append events and maintain the
projections.

### Rollout

Existing tasks are **not** migrated — there is no backfill, so old tasks carry
no stream history, and any tasks in flight at cutover are drained or re-created
rather than translated. Reusing the freed `events` name means the old and new
`events` tables can't coexist, so the schema change and the code cutover ship
together rather than as separate additive steps.

1. **Schema.** One migration (next sequence after
   `20260607000002_task_auto_archive.sql`) drops the old org-level `events`,
   `event_tasks`, and `tasks.instructions`; creates the new `events` stream
   table under the reused name; and narrows `logs` to the verbose channel. Keep
   `task_links` (the subscription index). Regenerate `sqlc`.
2. **Cutover.** In the same deploy, re-point every mutation site that today
   writes instructions/logs/events/links/status to append the corresponding
   `events` row in the same transaction (the `TaskChange` value type from the
   sibling proposal is the natural constructor for `lifecycle` rows), and point
   the brief, `get_my_task`, the org event feed (`ListEvents`, now org-scoped
   over the stream), and the #918 timeline at `ListEvents`. The initial
   instruction becomes the first `instruction` event appended by `CreateTask`.

## Trade-offs

- **One verbose channel vs. one unified table with a `volume` tag.** Keeping
  `logs` separate is less migration churn, keeps the brief/timeline-critical
  queries off the high-volume rows, and leaves
  `proposals/draft/richer-tool-call-logs.md` to evolve the verbose channel
  independently. A single table with `volume ∈ {semantic, verbose}` is
  conceptually tidier and gives the timeline a single source, but every brief
  and subscription query would then have to filter past transcript chunks, and
  a runaway transcript would bloat the index the brief and timeline scans rely on.
  Recommend the separate channel; the type system still makes the distinction
  explicit (which table you're in).

- **Global `BIGSERIAL` id vs. per-task sequence.** A global id keeps the schema
  trivial and ordering total; filtering by `task_id` preserves order, so the id
  is also monotonic within a task and no per-task sequence is needed. The
  concurrent-insert visibility wrinkle (a higher id committing visible before a
  lower one) only matters for incremental cursor reads, which this proposal
  doesn't do — clients fetch the whole stream each time. Deferred to #946.

- **Materialized projections (`status`, `task_links`) vs. pure replay.**
  Pure event-sourcing would derive status and subscriptions by folding the
  stream on every read. Rejected: `ListTasks` and the runner poll are hot,
  indexed, list-shaped queries that must not replay per-row. Materializing the
  two projections that have hot read paths — and *only* those — keeps the
  source-of-truth/projection split honest without paying replay cost where it
  hurts.

- **Keeping `command`/`version` off the stream.** They are control state for
  runner coordination, not task history, and they mutate in place (a restart
  bumps `version`; the stream would otherwise accumulate churn). Modeling them
  as events would conflate "what the runner should do next" with "what
  happened." Left as columns.

## Open Questions

- **Does `instruction` keep a denormalized column?** Dropping
  `tasks.instructions` entirely means the initial prompt is reconstructed from
  the stream. The brief (to-agent events) covers the running
  agent, but anything that wants "the task's instructions" outside a run (e.g.
  list views, search) would scan the stream. A denormalized
  `tasks.first_instruction` or a kept column as a projection is cheap insurance
  — decide when writing the cutover migration.

- **Status fold ownership.** Should `Task.ApplyRunnerEvent` (and `Start` /
  `Restart` / `Cancel`) keep mutating `status` directly with the event appended
  beside it, or should there be a single `applyLifecycleEvent(task, ev)` fold
  that both appends and projects, so the projection can never drift from the
  stream? The latter is cleaner but a larger refactor of `internal/model/task.go`.

- **External-event fan-out volume.** One org-level webhook matching N subscribed
  tasks now appends N `external` events (one per task's stream) instead of one
  org-level `events` row + N `event_tasks` join rows. That drops the join and
  the separate org-level table; the org event feed instead reads `events WHERE
  org_id = ? AND type = 'external'` over the `idx_events_org_id` index. The same
  occurrence therefore appears once per subscribed task in the feed — accepted
  duplication (see Design). Worth confirming the fan-out volume (mass-subscribed
  repos) stays bounded.

- **Relationship to #945 (C2-hosted agent MCP).** With the agent contract
  reduced to append/read-stream over the task token, the C2-hosted MCP server
  (#945) and a bring-your-own-agent both implement the same two verbs. Should
  `AppendEvent`/`ListEvents` be the *public* agent contract, with the
  MCP tools as one client of it? Likely yes, but that is #945's call.
