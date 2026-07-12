# C2-Rendered Task Brief with Server-Side Delivery Cursors

Issue: https://github.com/icholy/xagent/issues/946

## Problem

When an agent starts — or restarts after a new instruction or external event —
it has to call `get_my_task` to learn *why* it was started. Everything that call
returns (instructions, new events, links) is already known to the C2 at dispatch
time: it is the reason the task is being run at all.

Today the bootstrap prompt (`internal/agent/PROMPT.md`, rendered by
`Config.prompt` at `internal/agent/driver.go:244`) nags the model into fetching
its own context:

```
# first run
Use xagent:get_my_task to fetch your task instructions and execute them.
# wake
The task was updated. Check xagent:get_my_task and continue.
```

Two problems follow:

1. **It's a convention, not a contract.** The wake only works if the prompt nags
   the model into calling the tool and the model complies. A harness that renders
   the `get_my_task` invoke block as literal text and exits has silently dropped
   the one thing the wake existed to deliver. Every agent implementation has to
   re-discover this ritual.

2. **It costs a turn.** The model burns a turn fetching context that could have
   been handed to it in the prompt it already received.

This proposal **pushes** the context instead of pulling it. The C2 renders a
**task brief** — task metadata, instructions, and the events since the last
delivery — and the dispatch path injects it into the agent's initial prompt
before exec. The C2 owns the rendering so all agent implementations get identical
context, and delivery is tracked by a **server-side cursor** so a restart doesn't
replay events the agent already received.

### Relationship to `wake-prompt-event-injection.md`

A sibling draft, `proposals/draft/wake-prompt-event-injection.md`, addresses the
same issue with a deliberately **narrower** slice: it keeps the cursor
*driver-local* (a `next_event_token` in the sandbox config file), injects raw
event JSON only on the **wake** path, and leaves first-run bootstrap on
`get_my_task`. That draft's own Trade-offs section names the design in this
document as the alternative it defers:

> If we later want a server-authoritative "what has this task's agent seen" for
> the Web UI or for multi-consumer delivery, a server-side cursor is the better
> home; for the single-driver wake path, sandbox-local is simpler.

This proposal is that server-authoritative design. It is the fuller #946 vision:
the **C2** renders the brief (so bring-your-own agents get it too — see
[Interaction with #945](#interaction-with-945)), the cursor lives in the
**database** (so it survives a container recreate and works for runtimes with no
xagent sandbox filesystem), and **first-run** context is pushed too, retiring
`get_my_task` from the required startup path entirely. The two drafts are
alternatives for the same issue; a reviewer should pick one. The Trade-offs
section weighs them directly.

### A note on "children summary"

The issue lists "children summary" as part of the brief. Child tasks were removed
from the model since the issue was filed (`20260613000001_drop_task_parent.sql`;
`GetTaskDetailsResponse` still carries `reserved 2; // Previously: children` in
`proto/xagent/v1/xagent.proto`). The brief therefore covers **task metadata,
instructions, events, and links** — there is no child summary to render. If child
tasks return, the brief renderer is the natural place to add them.

## Design

### 1. The task brief: what the C2 renders and its shape

The brief is rendered **server-side** by a new pure helper, so every agent
runtime gets byte-identical context regardless of how it connects. It reuses the
exact data `GetTaskDetails` already assembles (`GetTaskDetails` in
`internal/server/apiserver/task.go:188`):

- **Task metadata** — `id`, `name`, `status`, `workspace`, `namespace`, `url`
  (from `task.Proto(s.baseURL)`).
- **Links** — the task's `task_links` rows (`ListLinksByTask`), so the agent
  knows which resources it is subscribed to without a tool call.
- **Events since the cursor** — the to-agent events (`instruction` + `external`)
  with `id` greater than the task's delivered cursor, in stream order. On first
  run the cursor is `0`, so this is the full instruction set that seeded the task
  (`CreateTask` writes each instruction as an `InstructionPayload` event with
  `Wake: true`); on a wake it is only what arrived since the previous run.

Rendering lives in a new `model` helper next to the existing event-rendering code
(`LifecyclePayload.Summary()` in `internal/model/event.go:213` already renders
events to human-readable lines):

```go
// internal/model/brief.go
//
// RenderBrief produces the prompt-ready task brief the C2 hands the agent at
// dispatch. events must already be filtered to the to-agent types and to those
// after the delivery cursor; the caller supplies them so rendering stays pure.
func RenderBrief(task *Task, events []*Event, links []*Link) string
```

Output is **markdown**, not raw JSON — the C2 owning a single rendered shape is
the point (identical context for a LangChain agent and a Claude Code agent). A
sketch:

```markdown
# Task 1289: Proposal: inject task brief (#946)
Status: RUNNING · Workspace: xagent · https://xagent.choly.ca/ui/tasks/1289

## Instructions
1. Write a design proposal for GitHub issue #946 ...

## New since you were last here
- [external] GitHub PR review comment on #947
  https://github.com/icholy/xagent/pull/947#discussion_r123
  path=internal/agent/driver.go line=149

## Links
- PR you opened (subscribed): https://github.com/icholy/xagent/pull/947
```

Choosing rendered markdown over the raw `protojson` array that `get_my_task`
returns is the deliberate divergence from `wake-prompt-event-injection.md`; the
trade-off is discussed [below](#trade-offs).

### 2. Server-side delivery cursor (the load-bearing part)

The requirement is: **once an event is included in a brief the agent received, a
restart must not replay it.** Today nothing tracks this — every restart re-runs
`GetTaskDetails` and re-delivers the whole stream, leaning on the model to figure
out what's new. We make "new since last delivery" a property of the system.

#### Schema change

A high-water mark on the task row — the id of the newest event already delivered
to the agent in a brief it durably received:

```sql
-- internal/store/sql/migrations/20260713000001_task_brief_cursor.sql
-- migrate:up
ALTER TABLE tasks ADD COLUMN brief_cursor bigint NOT NULL DEFAULT 0;

-- migrate:down
ALTER TABLE tasks DROP COLUMN IF EXISTS brief_cursor;
```

`brief_cursor = 0` is the natural first-run state ("nothing delivered yet"), and
`events.id` is a unique monotonic `bigserial` (see the `eventCursor` comment in
`internal/store/event.go:92`), so a single `bigint` is a total order over the
stream — no composite cursor, no timestamp tiebreak. It goes on `tasks`, not
`events`: it is one scalar per task, not per-event bookkeeping, and it rides the
existing `tasks` row the state machine already loads for update.

#### Where the cursor is read

`GetTaskBrief` (new RPC, [below](#3-dispatch-the-brief-rpc)) reads events where
`id > brief_cursor`, filtered to `instruction` + `external`. This is a small
addition to `ListEventsByTask` — an `after_id` predicate alongside the existing
`types` filter — or a dedicated `ListEventsByTaskAfter(task_id, after_id, types)`
store method backed by `WHERE task_id = $1 AND org_id = $2 AND id > $3 AND
(type = ANY($4) OR cardinality($4) = 0) ORDER BY id`. The response carries the
**max included event id** as `delivered_event_id` so the driver can later report
what it received.

`GetTaskBrief` is a **pure read** — it does **not** advance the cursor. Advancing
at read time would be at-most-once: an agent that fetched the brief and then
crashed before acting on it would have those events marked delivered and never
replayed — a silent miss, exactly the failure the cursor exists to prevent.

#### Where the cursor is advanced

The cursor advances when the run that received the brief **terminates cleanly**,
folded into the same status-guarded transition that already lands the task in
`completed`. `RunnerEvent` gains an optional field:

```go
// internal/model/task.go
type RunnerEvent struct {
    TaskID    int64
    Event     RunnerEventType
    Version   int64
    Reconcile bool
    Reason    string
    // DeliveredEventID is the max event id the driver injected into the agent's
    // brief this run. Advances tasks.brief_cursor when a stopped event lands the
    // task in completed. Zero means "no brief delivered" (old drivers) — no-op.
    DeliveredEventID int64
}
```

The advance hooks the one transition in `applyRunnerEventStopped`
(`internal/model/task.go:271`) where a clean run finishes —
`Running + command=none → Completed` (line 289–292):

```go
if t.Command == TaskCommandNone {
    t.Status = TaskStatusCompleted
    if e.DeliveredEventID > t.BriefCursor {
        t.BriefCursor = e.DeliveredEventID
    }
    return true
}
```

Because the advance is gated on the same status guard as the transition, the
superseded cases are handled for free:

- **Clean completion** (`Running+none → Completed`): guard accepts → cursor
  advances to what the agent received. The next natural wake fetches only events
  after it — no replay.
- **Restart mid-run** (`Restarting+restart`): the old run's `stopped` is a no-op
  (the guard rejects it — see the driver-owned-events state machine), so the
  cursor does **not** advance. The new run re-fetches from the old cursor and
  re-delivers, which is correct: the interrupted run may not have finished
  processing those events.
- **Failure** (`stopped` never sent; `failed` instead): `applyRunnerEventFailed`
  does not touch the cursor, so a failed run's events are re-delivered on retry.

This is **at-least-once** delivery — the same "duplicates are safe" bias the
accepted `driver-owned-events` design takes. Advancing after the run (not before)
means a mid-run crash re-injects; an injected event the agent already handled is a
cheap re-read, whereas a dropped event is a task that never learns why it woke.

### 3. Dispatch: the brief RPC and where the driver injects it

A new read RPC returns the rendered brief plus the high-water mark:

```proto
// proto/xagent/v1/xagent.proto
message GetTaskBriefRequest {
  int64 id = 1;
}

message GetTaskBriefResponse {
  string brief = 1;             // rendered markdown, ready to inject
  int64 delivered_event_id = 2; // max event id included; 0 if none
}

rpc GetTaskBrief(GetTaskBriefRequest) returns (GetTaskBriefResponse);
```

The handler mirrors `GetTaskDetails`: load the task, authorize with
`OpTaskRead`, read `links` and the `instruction`+`external` events after
`task.BriefCursor`, and return `RenderBrief(...)` plus the max included id. It is
authorized by the same task-scoped JWT the driver already presents (`task.read`).

**Where it's injected.** `runAgent` (`internal/agent/driver.go:149`) already loads
the config and builds the prompt via `cfg.prompt()` before `a.Prompt(ctx, prompt,
cfg.Started)`. The brief fetch slots in between, and the driver already holds the
task and client it needs:

1. **Fetch** `GetTaskBrief(task_id)`. Keep the returned `delivered_event_id`.
2. **Inject.** `PROMPT.md` gains a `Brief` field; when non-empty the template
   renders the brief in place of the `get_my_task` nag. The template's data struct
   grows one field alongside `Started`/`Prompt`:

   ```go
   err := promptTemplate.Execute(&b, struct {
       Started bool
       Prompt  string
       Brief   string // rendered task brief; empty when the RPC returned nothing
   }{ ... })
   ```

3. **Exec** the agent with the brief already in its context.
4. **Report** the high-water mark on the terminal `stopped` submit. The driver
   already emits `stopped` and waits for the ack (`Driver.Run` /`Driver.submit`,
   `internal/agent/driver.go:96-111`); it sets `event.DeliveredEventID =
   deliveredEventID` on that event. The cursor advance is thus transactional with
   the completion ack — one round-trip, no extra RPC.

No change to the runner or the config-file contract: the runner still writes the
same `Config` (`internal/runner/runner.go:436` `spec`) and the driver still owns
the prompt. The cursor is entirely server-side, so nothing new is persisted in the
sandbox.

### 4. `get_my_task` demotes to a mid-run refresh

`get_my_task` stays registered and behaviorally unchanged
(`internal/agentmcp/xmcp.go`), but its role changes:

- **Start-time context is pushed** — the brief in the initial prompt. The agent
  starts already knowing its instructions, links, and the events that woke it. No
  first-turn tool call required.
- **Mid-run context is pulled** — a long-running agent that wants to ask "did
  anything arrive while I was working?" still calls `get_my_task`. It returns the
  full current details (all events, not cursor-scoped), which is exactly right for
  an on-demand refresh.

`get_my_task` **does not** advance `brief_cursor` — pulling is an idempotent read;
only the pushed brief (acked via the terminal `stopped`) marks events delivered.
This keeps the two paths cleanly separated: the cursor tracks what was *pushed*,
and the pull is always free to re-read everything.

`PROMPT.md` is rewritten accordingly — the mandatory "call `get_my_task` first"
line is gone; a short note documents `get_my_task` as an optional mid-run refresh.
The reliability win is that the **startup path no longer depends on a tool call**:
the events that justify the run are in the prompt text the model already reads, so
a harness that fails to execute a tool call can no longer silently drop them.

### 5. Backward compatibility

- **Additive schema.** `brief_cursor` defaults to `0`; existing tasks behave as
  "nothing delivered yet." The first brief after the migration re-delivers the
  instruction/external stream once (harmless), then tracks incrementally.
- **Additive RPC + field.** `GetTaskBrief` is new; `RunnerEvent.DeliveredEventID`
  defaults to `0`. A driver built before this change never calls `GetTaskBrief`
  and never sets `DeliveredEventID`, so it keeps working on the `get_my_task`
  path and never advances the cursor — no coordinated deploy required. A new
  driver against an old server degrades to a `GetTaskBrief` unimplemented error,
  which the driver treats as "no brief" and falls back to the `get_my_task` nudge.
- **No state-machine semantics change.** The cursor advance is bolted onto the
  existing `Running+none → Completed` transition and is a no-op when
  `DeliveredEventID == 0`. Every existing transition, guard, and idempotency
  property in `internal/model/task.go` is untouched.

### Interaction with #945

#945 (host the agent-facing MCP server on the C2) and this proposal are the two
halves of a public agent contract: #945 makes the xagent tools reachable from any
runtime, and this makes the startup context reachable without any tool call.

The **server-side cursor is what makes the brief work for #945's bring-your-own
agents.** A LangChain/K8s/ECS agent has no xagent sandbox and no config file — so
the driver-local `next_event_token` of `wake-prompt-event-injection.md` has
nowhere to live for those runtimes. Because this design keeps the cursor in the
database and renders the brief C2-side, the same brief can be served:

- over the **C2-hosted MCP endpoint** from #945 (a `get_task_brief` tool, or the
  brief returned on session open), advancing the cursor when the session reports
  completion; or
- via the `GetTaskBrief` RPC directly, for a driver like today's.

The rendering and the cursor live in exactly one place (the C2) regardless of who
consumes them, which is the property #945 needs and the driver-local design cannot
offer. If both land, `GetTaskBrief` is the shared core and #945's MCP tool is a
thin wrapper over it.

## Implementation Plan

1. **Schema migration** — Delivers: `tasks.brief_cursor bigint NOT NULL DEFAULT 0`
   and the up/down migration. Depends on: nothing. Verifiable by: `dbmate up`/`down`
   run cleanly and `schema.sql` regenerates with the column.

2. **Store: cursor read + advance** — Delivers: `Task.BriefCursor` on the model,
   an `after_id` predicate on the task-scoped event query (or a new
   `ListEventsByTaskAfter`), and the `applyRunnerEventStopped` advance on the
   `Completed` transition. Depends on: (1). Verifiable by: store unit tests
   (events after a cursor) and a `task_test.go` case asserting the cursor advances
   only on `Running+none → Completed` and not on the restart-superseded `stopped`.

3. **Brief renderer** — Delivers: `model.RenderBrief(task, events, links) string`
   and its golden tests. Depends on: nothing (pure function over model types).
   Verifiable by: table-driven tests rendering briefs for first-run (all
   instructions), wake (events after cursor), and empty-events cases.

4. **`GetTaskBrief` RPC** — Delivers: the proto messages/RPC, generated code, and
   the handler (load task, authorize `OpTaskRead`, read links + events after
   `brief_cursor`, return `RenderBrief` + `delivered_event_id`). Depends on: (2),
   (3). Verifiable by: a handler test asserting the brief and `delivered_event_id`
   for a task with instruction + external events. **Independently shippable** —
   anyone can call `GetTaskBrief` before the driver does.

5. **`RunnerEvent.DeliveredEventID` wiring** — Delivers: the proto field on
   `RunnerEvent`, `model.RunnerEvent.DeliveredEventID`, and its plumb through
   `Proto()`/`RunnerEventFromProto` so `SubmitRunnerEvents` carries it. Depends
   on: (2). Verifiable by: a `SubmitRunnerEvents` handler test asserting a
   `stopped` with `DeliveredEventID = N` advances `brief_cursor` to `N`. Safe to
   merge before (6): until the driver sets it, it is always `0` (no-op).

6. **Driver injects the brief** — Delivers: the `GetTaskBrief` call in `runAgent`,
   the `Brief` template field, the `PROMPT.md` rewrite, and setting
   `DeliveredEventID` on the terminal `stopped` submit. Depends on: (4), (5).
   Verifiable by: `internal/agent/driver_test.go` — the prompt contains the brief
   when the RPC returns one, falls back to a bare nudge when it returns nothing,
   and the emitted `stopped` carries the delivered id.

7. **`get_my_task` / prompt cleanup** — Delivers: `PROMPT.md` wording that
   documents `get_my_task` as an optional mid-run refresh rather than a required
   startup step, and the tool description update in `internal/agentmcp/xmcp.go`.
   Depends on: (6). Verifiable by: template rendering tests.

Layer 1 is the independent foundation; 2 builds the store/state-machine behavior
on it; 3 is a pure renderer with no dependency; 4 composes 2+3 into a callable RPC
that is useful on its own; 5 wires the advance field and is safe ahead of 6; 6
turns on the pushed brief end to end; 7 is the cleanup that retires the startup
tool call.

## Trade-offs

**Server-side cursor vs. driver-local token
(`wake-prompt-event-injection.md`).** The sibling draft keeps the cursor in the
sandbox config file: no migration, no new column, no write-path bookkeeping, and
it composes with the same-container-restart model where the file already persists
resume state. This design instead adds a `tasks.brief_cursor` column. The cost is
one migration and a field on `RunnerEvent`; the benefits are (a) the cursor
survives a container **recreate** (the driver-local token resets with the config
file), (b) the server has an authoritative record of what a task's agent has seen
— usable by the Web UI or future multi-consumer delivery, and (c) it is the only
option that works for **#945's runtimes with no xagent sandbox**. For the
single-driver wake path the driver-local token is genuinely simpler; this design
pays a small schema cost to buy the bring-your-own-agent future. **These are
competing proposals for #946 — pick one.**

**Rendered markdown vs. raw event JSON.** `wake-prompt-event-injection.md` injects
the exact `protojson` shape `get_my_task` already returns, so the agent parses one
format and there is zero new rendering code. This design renders markdown
server-side. The cost is a renderer to write and keep in sync; the benefit is that
the C2 owns one consistent shape for *every* runtime (a bring-your-own agent gets
the same brief a Claude Code agent does), and prose is more scannable than a JSON
array. Since #946 frames it as "the C2 renders a task brief ... so all agent
implementations get consistent context," rendered output is the closer fit to the
issue.

**Push first-run too vs. keep `get_my_task` bootstrap.** The sibling draft keeps
first-run on `get_my_task` because it needs task name/links/status beyond events.
This design folds that metadata into the brief, so first-run is pushed like every
wake and the startup tool call is fully retired. The cost is that the renderer
must include task metadata (it does); the benefit is a single code path and no
required startup tool call at all.

**Advance-after-run (at-least-once) vs. advance-at-read (at-most-once).**
Advancing the cursor when the run completes means a retried run re-injects.
Chosen deliberately over advancing at brief-fetch time, which would drop the
run's payload on any mid-run failure — the worse outcome for a mechanism whose
entire job is to deliver that payload.

## Open Questions

- **Does the brief ever include `lifecycle` events?** Today the brief is
  `instruction` + `external` only. A human status change while the agent slept is
  a `lifecycle` event the agent can't currently see pushed. Probably still no —
  lifecycle is internal timeline, not agent input — but worth confirming.

- **Cursor advance on `cancelled`?** The design advances only on
  `Running+none → Completed`. A cancelled task is done, so replay is moot, but for
  symmetry we could also advance on the `cancelling → cancelled` transition.
  Current stance: leave it out — simpler, and a cancelled task never wakes to
  replay anyway.

- **Should the brief RPC page?** A task with a very long instruction/event history
  since the cursor renders a large brief. In practice "since the last delivery" is
  small (one or two events per wake), and first-run is bounded by the seed
  instructions. If briefs grow, `GetTaskBrief` can cap the event window and note
  the truncation, deferring the tail to a `get_my_task` pull.

- **Supersede this draft or the sibling?** `wake-prompt-event-injection.md` and
  this document are alternatives for #946. If this one is accepted, the sibling
  should move to `proposals/rejected/` (or be closed) and vice versa. This
  decision should be made before either is implemented.
