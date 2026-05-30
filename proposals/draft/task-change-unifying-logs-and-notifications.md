# TaskChange: a single source for task-scoped logs and notifications

## Problem

Task-scoped logging and change notifications are two parallel, hand-written
systems that say nearly the same thing twice, and both are weak.

### 1. The log is sparse and low-information

Every task-mutating call site builds a `model.Log` by hand and calls
`store.CreateLog` inside its transaction. The strings are short, lossy, and
ceremonial:

- `apiserver/task.go:110` — `"<user> created task"`
- `apiserver/task.go:213` — `"<user> updated task: name, instructions"` (the
  `changed []string` slice joined as csv)
- `apiserver/task.go:261` — `"<user> archived task"`
- `apiserver/task.go:306` — `"<user> unarchived task"`
- `apiserver/task.go:351` — `"<user> cancelled task"`
- `apiserver/task.go:402` — `"<user> restarted task"`
- `apiserver/runner.go:51,86` — `toRunnerEventLog` returns one of three canned
  strings keyed by `RunnerEventType`:
  - `RunnerEventStarted` → `"container started"`
  - `RunnerEventStopped` → `"container exited successfully"`
  - `RunnerEventFailed` → `"container failed"`
- `eventrouter/eventrouter.go:135` — `"webhook started task"`
- `archiver/archiver.go:128` — `"auto-archived: archive_after deadline reached"`

The live UI log is dominated by repeated `container started` / `container
exited successfully` rows for runner-driven restarts, and `webhook started
task` rows that fire repeatedly with **no indication of which webhook, which
event, or what was said to wake the task**. A user scanning the log cannot tell
*why* a task kept waking or *what* changed each time. The cause exists in
scope at the call site (`event.Description`, `event.URL`, `changed []string`,
the resulting `task.Status`), but is thrown away before the log row is built.

### 2. The agent-channel message duplicates the log

The recently merged `Notification.ChannelMessage` field
(`internal/model/notification.go:20`, PR #725, prior art:
[`proposals/draft/summary-gated-channel-notifications.md`](summary-gated-channel-notifications.md))
is also hand-written at each publish site. Today, in `apiserver/task.go` and
`apiserver/runner.go`, every publish site that has both a `CreateLog` call and
a `ChannelMessage` assignment is computing the same underlying fact — "this
thing just happened to this task" — twice in two formats, separated by a few
dozen lines:

```go
// task.go:210-223 (UpdateTask, abbreviated)
s.store.CreateLog(ctx, tx, &model.Log{
    TaskID:  req.Id,
    Type:    "audit",
    Content: fmt.Sprintf("%s updated task: %s", caller.AuditName(), strings.Join(changed, ", ")),
})
// ...
if req.Start {
    notification.ChannelMessage = fmt.Sprintf("Task %d queued: %s.", task.ID, strings.Join(changed, ", "))
}
```

The two strings differ only by coincidence of wording. They share the same
inputs (`changed`, `task`, `caller`) and describe the same event from two
angles (audit vs. agent-actionable). Neither is structured; both have to be
re-derived if a new field needs to be surfaced (resulting status, attempt
number, runner id) in either output.

### 3. The two outputs derive from one underlying fact

The fact (`task N was woken by event E from actor A, transitioning to status
S, with resulting changes C`) is computed once in scope, then projected by
hand into two formats: a free-text audit log row and a free-text channel
message. We want a **single structured source** that produces both — and that
makes the log genuinely useful in the process.

## Design

### `model.TaskChange`

Introduce a value type in `internal/model` (new file
`internal/model/taskchange.go`) that captures the fact once. The name is
`TaskChange`; the rejected `TaskEvent` alternative is discussed at the end of
this section.

```go
package model

import (
    "fmt"
    "strings"
    "time"
)

// TaskChange is the structured record of a single thing that happened to a
// task. It is the single source that projects to both the persisted audit
// log row (Log) and the in-memory change notification (Notification).
//
// One TaskChange is constructed per task per atomic change at every site that
// today calls store.CreateLog or apiserver.Server.publish with a task in
// scope. The value is constructed inside the same transaction closure that
// updates the task, written to the log inside the transaction, and projected
// to a notification after commit.
type TaskChange struct {
    TaskID int64
    Kind   TaskChangeKind

    // Actor identifies who or what caused the event. Populated from the
    // apiauth.UserInfo at API entry points; "runner" / "webhook" /
    // "archiver" for internal subsystems.
    Actor Actor

    // Status is the task's status AFTER the change was applied. Meaningful
    // for kinds that can transition (lifecycle, status-mutating commands);
    // carried verbatim from task.Status for the others so the projection
    // can always read it.
    Status TaskStatus

    // Changed is the set of fields mutated by an UpdateTask-style call.
    // Used by Kind == TaskChangeUpdated to render "name, instructions, status".
    Changed []string

    // Event, if non-nil, is the triggering external event for
    // TaskChangeWoken. Carries Description and URL into Log() / Notification().
    Event *Event

    // Exit, if non-nil, carries runner lifecycle context for
    // TaskChangeContainerExited / TaskChangeContainerFailed: the runner event
    // type and the resulting task status. Kept as a sub-struct so the
    // TaskChange stays compact for kinds that don't need it.
    Exit *ExitInfo

    // Time the event was observed. Set by the constructor; copied verbatim
    // into the projected Notification.
    Time time.Time
}

// Actor describes the cause of the event.
type Actor struct {
    Kind string // "user" | "runner" | "webhook" | "archiver" | "api_key"
    Name string // display name; "" for unattended actors
    ID   string // optional stable identifier (user id, runner id, key id)
}

// ExitInfo carries runner-lifecycle context.
type ExitInfo struct {
    Event RunnerEventType // started | stopped | failed
}
```

#### The `Kind` enum is closed

Every site is enumerated from the call-site survey below. The set is small
enough that a `Kind` enum (not a free-text string) is the right shape: each
kind has a known projection to log text and to channel-message gating.

```go
type TaskChangeKind int

const (
    TaskChangeCreated          TaskChangeKind = iota // CreateTask
    TaskChangeUpdated                               // UpdateTask (name / instructions / archive_after / start)
    TaskChangeCancelled                             // CancelTask (terminal or Cancelling)
    TaskChangeRestarted                             // RestartTask
    TaskChangeArchived                              // ArchiveTask (manual)
    TaskChangeUnarchived                            // UnarchiveTask
    TaskChangeAutoArchived                          // archiver.archive (deadline tick)
    TaskChangeWoken                                 // eventrouter.attach
    TaskChangeContainerStarted                      // RunnerEventStarted (applied)
    TaskChangeContainerExited                       // RunnerEventStopped (applied, with resulting Status)
    TaskChangeContainerFailed                       // RunnerEventFailed (applied)
)
```

This is closed and load-bearing. There is no free-text "other" kind: every
publish site that currently has a task in scope maps cleanly to one of the
above. The few `s.publish` sites that have no task in scope (event creation,
key management, workspace registration, org membership) stay as plain
`model.Notification` and are out of scope for `TaskChange` (see scope boundary
below).

The two existing task-event-adjacent kinds that do **not** become `TaskChange`
values:

- **`UploadLogs` agent log append** (`apiserver/log.go:31`). The agent uploads
  log entries it produced itself; these are already structured `model.Log`
  values with their own `Type` / `Content` from the agent's transcript. They
  are not "a thing that happened to the task" — they are the task's own
  output. They continue to go through `CreateLog` directly. The accompanying
  `s.publish` becomes a plain `model.Notification` with `Resources: [task_logs
  appended]` and no `ChannelMessage`, exactly as today.
- **`CreateLink`** (`apiserver/link.go:36`). Link creation is the agent
  attaching a reference to its own task, with no status transition and no
  call-site `CreateLog`. It produces a `Resources: [task_links, link]`
  notification only. Folding link-create into a `TaskChange` would force a
  log-on-every-link policy that no caller has asked for and would noisy the
  audit log with rows the user already sees in the Links pane.

These two exclusions are deliberate: `TaskChange` is the projection target for
sites that **today already write both** a log row and a publish. The few
publish-only or log-only sites without a peer remain as they are.

### Projections

The type owns two pure projections:

```go
// Log renders the audit-log row for this event. Always non-empty — every
// TaskChange logs. This is the key user-visible payoff: rich text replaces
// the canned "container started" / "webhook started task" lines.
func (e *TaskChange) Log() Log {
    return Log{
        TaskID:    e.TaskID,
        Type:      e.logType(),
        Content:   e.logContent(),
        CreatedAt: e.Time,
    }
}

// Notification projects the event into a model.Notification suitable for
// publication via apiserver.Server.publish. The envelope (OrgID, UserID,
// ClientID, Runner) is supplied by the caller via Envelope — those fields
// are caller state, not task fact, so the TaskChange does not carry them.
//
// ChannelMessage is populated only for agent-actionable kinds; log-only
// kinds (container_started, auto_archived, etc.) leave it empty, which the
// channel forwarder gates on. This generalizes the "empty ChannelMessage
// means silent" rule from PR #725 into a structural property of the kind.
func (e *TaskChange) Notification(env Envelope) Notification {
    return Notification{
        Type:           "change",
        Resources:      e.resources(),
        Time:           e.Time,
        OrgID:          env.OrgID,
        UserID:         env.UserID,
        ClientID:       env.ClientID,
        Runner:         env.Runner,
        ChannelMessage: e.channelMessage(),
    }
}

// Envelope is the caller/transport context that the TaskChange itself does
// not own.
type Envelope struct {
    OrgID    int64
    UserID   string
    ClientID string
    Runner   string // typically task.PendingRunner()
}
```

The unexported helpers `logType`, `logContent`, `resources`, and
`channelMessage` are a single `switch e.Kind` each, kept beside the kind
declaration so the table is reviewable as one unit.

#### Worked projection table

The behavior of `Log()` and `channelMessage()` for each kind, written out so
the rule is reviewable in one place:

| Kind | `Log().Content` | `channelMessage()` |
| --- | --- | --- |
| `Created` | `<actor> created task on <runner>/<workspace>` | `Task N created on <runner>/<workspace>.` |
| `Updated` | `<actor> updated task: <changed joined>` (+ `; started` if `Start` was set) | `Task N queued: <changed joined>.` iff `task.PendingRunner() != ""`, else `""` |
| `Cancelled` | `<actor> cancelled task` (+ `; cancellation requested` if still Running) | `Task N cancelled.` if `Status == Cancelled`; `Task N cancellation requested.` if `Status == Cancelling`; else `""` |
| `Restarted` | `<actor> restarted task` | `Task N restart requested.` |
| `Archived` | `<actor> archived task` | `""` (admin housekeeping, terminal already announced) |
| `Unarchived` | `<actor> unarchived task` | `""` |
| `AutoArchived` | `auto-archived: archive_after deadline reached` | `""` |
| `Woken` | `woken by event N: <event.Description> (<event.URL>)` | `Task N woken by event N: <event.Description> (<event.URL>)` |
| `ContainerStarted` | `container started (attempt N, status: <Status>)` | `""` (lifecycle: log-only) |
| `ContainerExited` | `container exited; task <Status>` (i.e. `task completed`, `task pending` for re-queue, `task cancelled`) | `Task N completed.` if `Status == Completed`; `Task N cancelled.` if `Status == Cancelled`; else `""` |
| `ContainerFailed` | `container failed; task <Status>` | `Task N failed.` if `Status == Failed` else `""` |

Two things the table makes explicit that the current code does not:

1. **`container exited successfully`** is replaced by `container exited; task
   pending` (re-queue), `container exited; task completed`, `container exited;
   task cancelled`, depending on what `applyRunnerEventStopped`
   (`model/task.go:230-260`) decided. The resulting status is the user's
   answer to "did the container finish the work, or is it restarting?" —
   today they cannot tell without correlating the row with subsequent log
   lines.
2. **`webhook started task`** is replaced by `woken by event 17: PR comment
   from alice on icholy/xagent#481: please rebase
   (https://github.com/icholy/xagent/pull/481#issuecomment-...)`. The
   constructor in `eventrouter.attach` has `event` in scope; today's log line
   discards both `Description` and `URL`.

#### Lifecycle entries are kept, not removed

The user explicitly wants the runner-lifecycle log entries retained. They are
useful as a coarse timeline of container restarts; the issue today is that
they're context-free, not that they're noise. The `ContainerStarted` /
`ContainerExited` / `ContainerFailed` kinds therefore continue to produce one
log row each — just with `Status` and (for restarts) attempt context, rather
than canned text. They do not produce channel messages: that gate already
exists in `apiserver/runner.go:63-70` and stays.

### The `ChannelMessage == "" → silent` rule, generalized

PR #725 introduced the rule that `mcpserver.ForwardNotification` (now inlined
at `internal/command/mcp.go:86`) suppresses delivery when
`Notification.ChannelMessage` is empty. Today, every publish site decides ad
hoc whether to set the field — sometimes via `task.go:222 if req.Start`,
sometimes via `runner.go:63 switch task.Status`, sometimes by simply omitting
the assignment.

Under this proposal the gate becomes a **structural property of the kind**.
Each kind's `channelMessage()` method returns either a sentence or `""`; the
publish site has no decision to make. The set of "agent-actionable" kinds is
the same set as today's PR-#725 rule (Created, Restarted, Updated-when-queued,
Cancelled-when-terminal, Woken, ContainerExited-when-terminal,
ContainerFailed-when-terminal) and the set of "silent" kinds is the same
(Archived, Unarchived, AutoArchived, ContainerStarted, non-terminal exits).
The semantics of PR #725 are preserved; what changes is that the decision is
named (per `Kind`) rather than re-derived per call site.

### Accumulation in `UpdateTask`

`UpdateTask` (`apiserver/task.go:176-236`) is the one site that handles
multiple changes in a single call: a single RPC can set `Name`, append
`Instructions`, set `ArchiveAfter`, and call `Start()`. Today it accumulates
those into a `changed []string` slice and joins them into one log line and
one channel message.

The `Updated` kind preserves that shape:

```go
change := model.TaskChange{
    TaskID:  task.ID,
    Kind:    model.TaskChangeUpdated,
    Actor:   actorFromCaller(caller),
    Status:  task.Status,
    Changed: changed, // accumulated inside the closure, as today
    Time:    time.Now(),
}
if err := s.store.CreateLog(ctx, tx, change.Log().Ptr()); err != nil {
    return err
}
```

(A `Log().Ptr()` shim or refactor of `store.CreateLog` to take a value is a
minor implementation detail.)

`Updated.Log().Content` becomes `"<actor> updated task: name, instructions;
started"` when `Start` was set, and `"<actor> updated task: name,
instructions"` when it wasn't. `Updated.channelMessage()` returns `"Task N
queued: name, instructions."` only when `task.PendingRunner() != ""`, i.e.
when `Start` queued runner work — the same condition PR #725 already uses.

No multi-event-per-call shape is needed; `UpdateTask` is the only accumulator
site. Other sites (CreateTask, ArchiveTask, runner events, etc.) produce
exactly one `TaskChange` per call by construction.

### The transaction-boundary seam

Logs are written inside the transaction via `store.CreateLog`; notifications
are published after commit via `s.publish`. The `TaskChange` value spans this
boundary: it is built once inside the `WithTx` closure, used immediately to
write the log row inside the transaction, and stashed in an outer-scope
variable for use after commit:

```go
var change model.TaskChange
err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
    task, err := s.store.GetTaskForUpdate(ctx, tx, req.Id, caller.OrgID)
    if err != nil { return err }
    // mutate task...
    if err := s.store.UpdateTask(ctx, tx, task); err != nil { return err }
    change = model.TaskChange{
        TaskID: task.ID, Kind: model.TaskChangeCreated, /* ... */
        Time: time.Now(),
    }
    if err := s.store.CreateLog(ctx, tx, change.Log().Ptr()); err != nil { return err }
    return tx.Commit()
})
if err != nil { return nil, ... }
s.publish(change.Notification(model.Envelope{
    OrgID:    caller.OrgID,
    UserID:   caller.ID,
    ClientID: caller.ClientID,
    Runner:   task.PendingRunner(), // captured before commit; pre-commit value
}))
```

This matches the existing structure of every handler in `apiserver/task.go`
(the `notification` value is already declared above the `WithTx` and filled
inside the closure). The shape of the diff at each call site is "replace two
hand-written values with one `TaskChange` construction, then call its two
projections."

For `eventrouter.attach` (`internal/eventrouter/eventrouter.go:117`), the
same pattern applies: `change` is built inside the closure with `Event: event`
populated from the second parameter, `Log()` is written inside the tx, and
`Notification()` is projected after commit and handed to `r.publish`.

### Envelope as argument, not field

The notification carries caller/transport context (`OrgID`, `UserID`,
`ClientID`, `Runner`) that is **not** a fact about the task — it's a fact
about the request. Two options:

- (a) `TaskChange` carries an `Envelope` field, populated by the constructor.
- (b) `Notification(env Envelope)` takes the envelope as an argument.

**Recommend (b).** The envelope changes between authoring and projection in
one important case: `Runner` is `task.PendingRunner()`, which is a derived
value from the post-commit task state — keeping it out of the `TaskChange`
makes it explicit that the caller decides which envelope to attach (and lets
the runner field reflect the final committed task, since the closure sets
`Runner: task.PendingRunner()` after the task has been mutated). It also
keeps `TaskChange` JSON-clean: the type can be logged or printed without
leaking caller identity.

### Call-site survey

Surveyed with `rg "CreateLog\(|\.[Pp]ublish\(" internal/`. Task-scoped sites
listed; non-task `s.publish` sites are out of scope (see scope boundary).

| File:line | Site | Current log | Current ChannelMessage | TaskChange Kind | Rich Log | Notifies? |
| --- | --- | --- | --- | --- | --- | --- |
| `apiserver/task.go:107,120` | `CreateTask` | `<actor> created task` | `Task N created on R/W.` | `Created` | `<actor> created task on R/W` | yes |
| `apiserver/task.go:210,234` | `UpdateTask` | `<actor> updated task: <changed>` | `Task N queued: <changed>.` (only when `Start`) | `Updated` | `<actor> updated task: <changed>[; started]` | yes iff queued |
| `apiserver/task.go:258,279` | `ArchiveTask` | `<actor> archived task` | (none) | `Archived` | unchanged | no (silent) |
| `apiserver/task.go:303,324` | `UnarchiveTask` | `<actor> unarchived task` | (none) | `Unarchived` | unchanged | no (silent) |
| `apiserver/task.go:348,375` | `CancelTask` | `<actor> cancelled task` | `Task N cancelled.` (only when Pending→Cancelled) | `Cancelled` | `<actor> cancelled task[; cancellation requested]` | yes iff terminal-or-Cancelling-with-runner |
| `apiserver/task.go:399,421` | `RestartTask` | `<actor> restarted task` | `Task N restart requested.` | `Restarted` | unchanged | yes |
| `apiserver/runner.go:51,80` | `SubmitRunnerEvents` (`toRunnerEventLog`) | one of `container started` / `container exited successfully` / `container failed` | terminal-status switch (`Task N completed/failed/cancelled.`) | `ContainerStarted` / `ContainerExited` / `ContainerFailed` | `container started (status: <Status>)` / `container exited; task <Status>` / `container failed; task <Status>` | only on terminal Status |
| `apiserver/event.go:45` | `CreateEvent` | n/a (no task) | n/a | — | (stays plain Notification) | — |
| `apiserver/event.go:78` | `DeleteEvent` | n/a (no task) | n/a | — | (stays plain Notification) | — |
| `apiserver/event.go:111` | `AddEventTask` (RPC) | n/a (no CreateLog) | n/a | — | (stays plain Notification) | — |
| `apiserver/event.go:147` | `RemoveEventTask` | n/a (no CreateLog) | n/a | — | (stays plain Notification) | — |
| `apiserver/link.go:36` | `CreateLink` | n/a (no CreateLog) | n/a | — | (stays plain Notification) | — |
| `apiserver/log.go:27,31` | `UploadLogs` | agent-supplied log entries | n/a | — | (stays as today; agent transcript) | — |
| `apiserver/key.go:36,67` | `CreateKey` / `DeleteKey` | n/a | n/a | — | out of scope | — |
| `apiserver/workspace.go:31,64` | `RegisterWorkspaces` / `ClearWorkspaces` | n/a | n/a | — | out of scope | — |
| `apiserver/org.go:108,143` | `Add/RemoveOrgMember` | n/a | n/a | — | out of scope | — |
| `eventrouter/eventrouter.go:135,157` | `attach` | `webhook started task` | `Task N woken by event E: <desc> (<url>)` | `Woken` | `woken by event E: <desc> (<url>)` | yes |
| `archiver/archiver.go:126,142` | `archive` (auto-archive tick) | `auto-archived: archive_after deadline reached` | (none) | `AutoArchived` | unchanged | no (silent) |

Eleven kinds; one closed enum; every site that currently writes both a log
and a publish becomes a single `TaskChange` construction.

### Runner lifecycle enrichment

`SubmitRunnerEvents` (`apiserver/runner.go:16-84`) is the most visible
payoff. The current flow:

```go
applied = task.ApplyRunnerEvent(&event)
if applied {
    if log, ok := s.toRunnerEventLog(event); ok {
        s.store.CreateLog(ctx, tx, &log)   // canned string
    }
    switch task.Status {                    // re-derives gate
    case TaskStatusCompleted: notification.ChannelMessage = ...
    // ...
    }
}
```

Under this proposal:

```go
applied = task.ApplyRunnerEvent(&event)
if !applied { return nil }
change := model.TaskChange{
    TaskID: task.ID,
    Kind:   kindFromRunnerEvent(event.Event),  // Started / Exited / Failed
    Actor:  Actor{Kind: "runner", Name: caller.Name},
    Status: task.Status,                       // resulting status in scope
    Exit:   &model.ExitInfo{Event: event.Event},
    Time:   time.Now(),
}
if err := s.store.CreateLog(ctx, tx, change.Log().Ptr()); err != nil { return err }
```

After commit:

```go
s.publish(change.Notification(model.Envelope{
    OrgID:    caller.OrgID,
    UserID:   caller.ID,
    ClientID: caller.ClientID,
    Runner:   task.PendingRunner(),
}))
```

`toRunnerEventLog` is deleted (its logic moves into `ContainerStarted /
ContainerExited / ContainerFailed`.Log()). The bespoke `switch task.Status`
that sets `ChannelMessage` is also deleted: the kind owns its own gate via
`channelMessage()`. A log row for a `Stopped` event with `Status == Pending`
(re-queue) now reads `container exited; task pending` instead of the
misleading `container exited successfully`.

### Scope boundary: task-scoped only

`model.Log` is task-scoped: every `Log` row has a `TaskID` (`store/sql/migrations/20240101000001_initial.sql:69`).
`model.Notification` is not: it covers org members, API keys, workspaces, and
event-only changes that have no task in scope. **This asymmetry is the reason
the new type is task-scoped.**

In-scope (becomes a `TaskChange`):

- Every site in `apiserver/task.go`, `apiserver/runner.go`
- `eventrouter/eventrouter.go` `attach`
- `archiver/archiver.go` `archive`

Out-of-scope (stays plain `model.Notification`):

- `apiserver/event.go` `CreateEvent`, `DeleteEvent`, `AddEventTask`,
  `RemoveEventTask`
- `apiserver/link.go` `CreateLink`
- `apiserver/log.go` `UploadLogs`
- `apiserver/key.go` `CreateKey`, `DeleteKey`
- `apiserver/workspace.go` `RegisterWorkspaces`, `ClearWorkspaces`
- `apiserver/org.go` `AddOrgMember`, `RemoveOrgMember`

These continue to construct `model.Notification` literals as today. No
`TaskChange`, no `ChannelMessage`, no log row. The presence of the
out-of-scope set is the reason for not merging `model.Log` and
`model.Notification` outright (see Alternatives below).

### Store / migration implications

**Recommend: free-text rendered logs, no schema migration in this proposal.**

`Log()` returns a `model.Log` whose `Content` is the rendered free-text
sentence. `store.CreateLog` (`internal/store/log.go:12`) and the underlying
`logs` table (`internal/store/sql/migrations/20240101000001_initial.sql:67`)
do not change. `model.Log` keeps `Type` and `Content` as opaque text. The DB
remains the audit trail; the rendered text remains the user-facing
representation.

Two reasons:

1. **Single source is achieved at the projection layer**, not the schema
   layer. The current duplication is in the *construction* of log strings and
   channel strings; making them flow from one `TaskChange` value fixes that
   without touching disk. A migration is not required to fix the listed
   problems.
2. **Structured-in-DB is a follow-up.** If the audit log later needs to be
   queryable by kind ("show me every `ContainerFailed` for task N",
   "aggregate `Woken` per source"), a follow-up proposal can add
   `logs.kind text NOT NULL` and `logs.payload jsonb`, backfill from
   `Content` parsing or leave old rows opaque, and have `Log()` populate the
   new columns. That work is orthogonal: the `TaskChange` value already
   carries the structure; only the persistence target would change.

Stating the recommendation explicitly: **first step is free-text-rendered;
structured-in-DB is deferred.**

### Backward compatibility

- **Existing log rows.** Untouched. `Content` is still free text; old rows
  read identically. New rows read with richer text.
- **Existing notification consumers.**
  - The **runner** (`xagentclient`, polls `Runner` field) sees no schema
    change; the `Notification` produced by `Notification(env)` has the same
    shape as today.
  - The **Web UI** (TanStack Query subscription, polls `Resources`) sees no
    schema change; `Resources` is populated by the same per-kind logic that
    today's hand-written publishes use.
  - The **agent channel** (`internal/command/mcp.go:86`) reads
    `ChannelMessage`; the per-kind gate produces the same set of sentences
    PR #725 produces today, so the channel sees the same set of frames it
    sees today (with rewordings where the proposal table differs from the
    current strings).
- **`ChannelMessage` field.** Stays on `model.Notification`. The field is
  *owned* by `TaskChange.Notification()` once the change lands — every
  task-scoped publish site sets it via the projection rather than by hand —
  but its position on the type and its consumer (the channel bridge) do not
  change. Non-task publishes leave it empty as they do today.
- **SSE wire format.** Unchanged. No proto changes (the projection works
  with the existing `model.Notification`, which is the type the SSE
  subscriber sees).
- **Migration.** None.

### Alternatives considered

**(a) Status quo: leave logs and notifications separate.** This is what's in
master today. The duplication is real but each instance is small; one could
argue the cost of a new shared type isn't worth it. Rejected because (i) the
*log* side is genuinely user-visible-bad (the sparse-log problem is the
loudest complaint) and we want to fix it whether or not we also unify the
notification, and (ii) every new task-mutating site added (e.g. a future
`AssignTask`, `ForkTask`) would have to re-derive the same two strings, which
is exactly the kind of repeated authoring this proposal removes.

**(b) Merge `model.Log` and `model.Notification` entirely.** Rejected because
`Notification` covers a broader non-task domain (workspaces, keys, org
members) that has no log analogue and shouldn't grow one. Coupling the types
would either bloat `Log` with non-task fields or force every workspace/key
notification through a fake "task event," neither of which is justified.
The asymmetry is real: every `Log` is task-scoped; not every `Notification`
is. `TaskChange` lives precisely in the intersection.

**(c) Structured logs in the DB up front.** Add `logs.kind`, `logs.payload
jsonb`, migrate every row, and re-render on read. Rejected as too large for
the first step: the migration touches every existing log row, the rendering
moves from "produced once at the source" to "produced on every read"
(performance and consistency risk), and the value (queryability) is
speculative. The single-source-with-projection design lets us deliver the
visible win (richer logs, unified author site) without that. Structured-in-DB
remains a clean follow-up once we know which queries we actually want.

**(d) Free-text `Kind` instead of an enum.** Considered; rejected. The set
of sites is closed and finite (eleven kinds), and the projection table is
the entire reason the type exists. A typo in a free-text kind would silently
break the log-rendering or channel-gating switch; the compiler should catch
those. The PR-#725 prior art makes the same closed-set argument for
`ChannelMessage` content (free-text was the right answer for the message
*body* because it varies per emission; an enum is the right answer for the
*kind* because the set is closed).

**(e) Name it `TaskEvent` instead of `TaskChange`.** Considered and rejected
in review. `TaskEvent` reads accurately for the runner-lifecycle and
webhook-wake kinds (which aren't user-driven changes — they're events the
task experienced from outside), but the codebase already has `model.Event`
for external triggers, and the runner subsystem already uses the word
"event" extensively (`model.RunnerEvent`, `RunnerEventStarted`,
`SubmitRunnerEvents`, `eventrouter`). A second `TaskEvent` type sitting next
to `model.Event` and `model.RunnerEvent` is a name-overload that reviewers
flagged immediately. `TaskChange` is unambiguous: every kind in the enum
(including `Woken`, which is the load-bearing borderline case) is a *change
the task underwent* as a result of an external trigger or an internal
transition, and the audit-log domain is exactly "things that changed."
Recommend `TaskChange`; the projection design is identical to the
`TaskEvent` variant.

## Test plan

The projections are pure functions of a `TaskChange` value, so most of the
test surface is table-driven over `(kind, populated fields) → (expected log
content, expected channel message, expected resources)`.

**`internal/model/taskchange_test.go`** (new):

- `TestTaskChange_Log_Projection` — table-driven over every `Kind` with a
  representative populated `TaskChange`. Asserts `change.Log().Content` matches
  the expected rendered string for each kind. Includes:
  - `Created` produces `<actor> created task on <runner>/<workspace>`.
  - `Woken` with `Event{Description: "PR comment from alice", URL:
    "https://github.com/.../481#..."}` produces the rich line including both
    description and URL.
  - `ContainerExited` with `Status: Pending` produces `container exited;
    task pending` (the re-queue case, distinguishing it from the
    `Completed` case).
- `TestTaskChange_Notification_ChannelMessage` — table-driven over every
  `Kind`. Asserts:
  - Agent-actionable kinds produce a non-empty `ChannelMessage` containing
    the task id.
  - Log-only kinds (`Archived`, `Unarchived`, `AutoArchived`,
    `ContainerStarted`, non-terminal `ContainerExited`) produce `""`.
  - `ContainerExited` with `Status: Pending` (re-queue) produces `""` — the
    log row fires but the channel stays silent until the *next* exit
    resolves to a terminal status.
- `TestTaskChange_Notification_Envelope` — assert the `Envelope` argument is
  copied verbatim into `OrgID`, `UserID`, `ClientID`, `Runner` and is not
  derived from the event itself.
- `TestTaskChange_Updated_Accumulation` — construct an `Updated` event with
  `Changed: []string{"name", "instructions", "status"}` and assert (i)
  `Log().Content` includes the comma-joined list, (ii) `channelMessage()`
  includes the joined list when `Runner` envelope is set (queued) and is
  `""` otherwise (name-only change).

**`internal/server/apiserver/runner_test.go`** (existing or new):

- `TestSubmitRunnerEvents_RichLogIncludesResultingStatus` — drive a `stopped`
  event into a `Running` task with `Command: Start` (re-queue case) and
  assert the log row reads `container exited; task pending` and the
  notification's `ChannelMessage` is `""`.
- `TestSubmitRunnerEvents_TerminalExitFiresChannelMessage` — drive a
  `stopped` event into a `Running` task with `Command: None` (clean
  completion) and assert the log row reads `container exited; task
  completed` and the channel message is `Task N completed.`

**`internal/server/apiserver/task_test.go`** (existing):

- `TestUpdateTask_TaskChange_LogAndChannelAgree` — call `UpdateTask` with
  `Name`, `AddInstructions`, and `Start: true`. Assert the persisted log row
  Content and the published Notification's ChannelMessage are both derived
  from the same `Changed` slice (no risk of them drifting). The test
  reasserts the PR-#725 invariant ("queued change → channel message") in
  the new structure.
- `TestUpdateTask_NameOnly_LogsButSilent` — call `UpdateTask` with only
  `Name` set. Assert the log row exists and reads `<actor> updated task:
  name`, and the notification's `ChannelMessage` is `""`.

**`internal/eventrouter/eventrouter_test.go`** (existing):

- `TestRouter_AttachLogIncludesEventDescription` — extend the existing
  `TestRouter_Attach...` to assert the log row's `Content` contains both
  `event.Description` and `event.URL`, not just `webhook started task`. The
  existing `ChannelMessage` assertion stays — the test now confirms both
  outputs come from the same projection.

**`internal/server/archiver/archiver_test.go`** (existing):

- `TestArchiver_AutoArchivedKind_Silent` — confirm the auto-archive path
  produces a log row but a notification with empty `ChannelMessage` (regression
  guard for the silencing decision).

No new test infrastructure is needed: the in-memory `pubsub` publisher
(`internal/pubsub/local.go`) captures notifications, and `model` package
tests are pure.

## Open Questions

- **Actor extraction.** `Actor{Kind: "user"|"runner"|"webhook"|...}` is built
  from `apiauth.UserInfo` (`AuditName()`, `Type`, `ID`) at API entry points
  and from hard-coded constants at internal subsystems (`archiver`,
  `eventrouter`). Is a small `model.ActorFromCaller(caller *apiauth.UserInfo)
  Actor` constructor justified, or should each site assemble the struct
  literal? Recommend the constructor for clarity but flagging as a small
  decision.
- **`Status: zero` for the no-status kinds.** `TaskStatusPending` is the
  zero value of `TaskStatus`, so `Status: 0` is ambiguous with "pending."
  For kinds that don't have a meaningful resulting status, the projection
  ignores `Status` and there's no observable issue, but reviewers may prefer
  a `*TaskStatus` to be explicit about "not applicable." Recommend leaving
  it as `TaskStatus` (zero ignored by the projection) for ergonomics; flag
  for review.
- **Truncation.** Same open question as PR #725: should `Log()` cap content
  length (e.g. 2KB) to defend against a runaway `event.Description`? Cheap
  insurance; default to be decided in implementation.
- ~~**`TaskEvent` vs `TaskChange`.**~~ Resolved in review: `TaskChange`,
  because `TaskEvent` collides with `model.Event` and `model.RunnerEvent`.
