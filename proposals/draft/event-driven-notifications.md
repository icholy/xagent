# Event-driven SSE notifications

Issue: https://github.com/icholy/xagent/issues/1028

## Problem

The server has two parallel mechanisms for "something happened to a task", and
they are decoupled:

1. **Task events** (`model.Event`, `internal/model/event.go`) — the durable,
   semantic, append-only stream landed by the unified-task-event-stream work.
   Lifecycle events carry a closed-set `LifecycleKind`, `FromStatus`/`ToStatus`,
   an `Actor`, and a `Message` (`LifecyclePayload`). This is the source of truth
   for *what happened*.

2. **SSE notifications** (`model.Notification`, `internal/model/notification.go`)
   — the thin "change" signal fanned out over `/events` by `notifyserver`. It
   carries only a list of `{action, type, id}` `Resources` plus a couple of
   opportunistically-attached fields (`for_runner`, `ChannelMessage`). It does
   **not** carry the semantics of what happened.

Both are produced side-by-side but independently. In `SubmitRunnerEvents`
(`internal/server/apiserver/runner.go`) the server appends a lifecycle `Event`
via `CreateEvent` **and**, a few lines later, hand-writes a `ChannelMessage`
string for terminal statuses — re-encoding, ad hoc and lossily, a fact the
lifecycle event already models precisely:

```go
// runner.go — inside the same tx that already created the lifecycle event
switch task.Status {
case model.TaskStatusCompleted:
    notification.ChannelMessage = fmt.Sprintf("Task %d completed.", task.ID)
case model.TaskStatusFailed:
    notification.ChannelMessage = fmt.Sprintf("Task %d failed.", task.ID)
case model.TaskStatusCancelled:
    notification.ChannelMessage = fmt.Sprintf("Task %d cancelled.", task.ID)
}
```

The same shape appears in `apiserver/task.go` (created / queued / archived /
cancelled / restarted), `eventrouter/eventrouter.go` (event attached / woken),
and `apiserver/runner.go` (terminal transitions). Each `ChannelMessage` is a
free-text restatement of a lifecycle or external event that was just written to
the stream.

Because the notification carries no event semantics, every consumer re-derives
meaning from thin signals:

- The **mcp channel bridge** (`internal/command/mcp.go`) gates on the free-text
  `ChannelMessage` and forwards it verbatim.
- The **runner** (`internal/command/runner.go`) ignores the contents entirely
  and just calls `Wake()`.
- A new consumer — the **`xagent notify` daemon** (#1025, PR #1026) — wants
  "only terminal task transitions (completed / failed / cancelled)". There was
  no clean way to express that, so the PR bolted a `TaskStatus` field onto
  `model.Notification` and stamped it in `SubmitRunnerEvents`, duplicating
  information the lifecycle event already holds. That is the smell this proposal
  removes: each new consumer need adds another semantic field to the
  notification, parallel to the event stream instead of derived from it.

This is the notification-side completion of the direction in
[`task-change-unifying-logs-and-notifications.md`](task-change-unifying-logs-and-notifications.md):
the *log* side became the event stream (lifecycle events replaced the
audit/info log rows); the *notification* side still re-derives `ChannelMessage`
by hand.

## Design

Make the SSE notification **reference the task event that caused it**, and have
consumers filter on event semantics instead of re-deriving them.

### 1. Carry the event on the notification

Add an optional `Event` to `model.Notification`. When a change is driven by a
task event, the publish site sets it to the event it just created in the same
transaction (so it already has its `ID`):

```go
// internal/model/notification.go
type Notification struct {
    Type      string                 `json:"type"`
    Resources []NotificationResource `json:"resources,omitempty"`
    Time      time.Time              `json:"timestamp"`
    OrgID     int64                  `json:"org_id"`
    UserID    string                 `json:"user_id,omitempty"`
    ClientID  string                 `json:"client_id,omitempty"`
    Runner    string                 `json:"for_runner,omitempty"`

    // Event is the task event that caused this notification, when the change
    // originated from the task event stream. nil for non-task changes (key,
    // org, workspace, bare log appends). Carries the typed payload, so
    // consumers filter on Event.Payload (lifecycle kind, to_status, actor)
    // instead of re-deriving from a free-text string.
    Event *Event `json:"event,omitempty"`

    Ignore bool `json:"-"`
}
```

`Resources` stays: it remains the cache-invalidation hint ("refetch task N /
its logs"). `Event` is the new semantic layer — *what* happened. The two are
complementary:

- `Resources` → "these resources changed, invalidate them" (web UI query cache,
  runner wake).
- `Event` → "this is the event that changed them" (channel bridge, notify
  daemon, future consumers).

`model.Event` already has a proto (`xagentv1.Event`) and round-trips through
JSON via its typed payloads, so the SSE layer (`notifyserver/sse.go`, which
`json.Marshal`s the notification) and `NotificationClient` (which
`json.Unmarshal`s it) need no changes beyond the new field. This also lines up
with #924 (protobuf SSE payloads): the embedded `Event` is already proto-backed.

### 2. Publish sites attach the event

Every site that creates a task event in a transaction and then publishes a
notification sets `notification.Event = event`. Concretely:

- `SubmitRunnerEvents` (`runner.go`) — set `notification.Event` to the
  lifecycle event returned by `event.LifecycleEvent(task, from)`
  (SANDBOX_STARTED / SANDBOX_EXITED / SANDBOX_FAILED). The hand-written
  terminal-status `switch` is **deleted**.
- `CreateTask` / `UpdateTask` / `CancelTask` / `RestartTask` / archive paths
  (`task.go`) — attach the corresponding lifecycle event; delete the
  `ChannelMessage` assignments.
- `eventrouter.go` — attach the `ExternalPayload` event (the webhook trigger)
  for the attach-only and wake paths; delete the `ChannelMessage` assignments.

Sites with no task event (workspace, key, org, bare log append) leave `Event`
nil and are unaffected.

### 3. Consumers filter on the event

**`xagent notify`** — emit a system notification only for lifecycle events that
represent a finished run. Add a helper so the rule lives in one place:

```go
// internal/model/event.go
// IsTerminal reports whether the lifecycle event represents a task reaching a
// terminal state: a sandbox exit into Completed/Failed, a sandbox failure, or
// a cancellation.
func (p *LifecyclePayload) IsTerminal() bool { ... }
```

The daemon's handler becomes:

```go
Handler: func(n model.Notification) {
    lc, ok := n.Event.Lifecycle()      // typed accessor, ok=false unless lifecycle
    if !ok || !lc.IsTerminal() {
        return
    }
    notify.Send(title, lc.Summary())   // reuse the existing renderer
}
```

`LifecyclePayload.Summary()` already renders human-readable lines ("Sandbox
exited (Running -> Completed)", "Cancelled by icholy"), so the daemon shows a
real message without any server-side `ChannelMessage`.

**mcp channel bridge** (`mcp.go`) — replace the `ChannelMessage == ""` gate with
an event-kind gate and render from the event (e.g. `Summary()`, or a
channel-specific renderer). The set of events forwarded to the agent channel
becomes an explicit policy on event kind rather than "whatever happened to have
a `ChannelMessage`".

**runner** and **web UI** — unaffected in behaviour. The runner still wakes on
any notification; the web UI already renders the timeline from events and reads
`Resources` for cache invalidation.

### 4. Retire `ChannelMessage` (and don't add `TaskStatus`)

Once both consumers read from `Event`, delete `Notification.ChannelMessage` and
all its publish-site assignments. PR #1026's `TaskStatus` field is never added —
the lifecycle event's `to_status` already carries it. The publish-side tests in
`apiserver/publish_test.go` (which currently `IgnoreFields(..., "ChannelMessage")`)
assert on `Event` instead.

## Trade-offs

- **Embed the full event vs. an event reference (id + kind).** Embedding the
  event means consumers need no follow-up RPC and can reuse `Summary()` and the
  typed payloads; the event is small. A bare `{id, kind}` reference would keep
  the notification minimal but force a `GetEvent` round-trip in every consumer
  that wants detail. Embedding wins for the known consumers; we can always drop
  to a reference later if payload size becomes a concern.

- **Keep `Resources` vs. derive everything from `Event`.** `Resources` is
  retained because not every notification maps to a single task event (log
  appends, link list changes, non-task resources), and the web UI uses it for
  query-cache invalidation independent of semantics. Folding it into `Event`
  would overload one field with two jobs.

- **Render in the daemon vs. on the server.** Moving rendering to consumers
  (`Summary()`) is what lets us delete the server-side `ChannelMessage`. The
  cost is that channel-message wording is no longer centralized on the server;
  the mitigation is that `Summary()` is already the shared Go renderer and the
  web UI has its parallel TS renderer, so wording already lives with the event.

## Open Questions

- **Channel-forward policy.** `ChannelMessage` today doubles as the gate for
  *which* changes reach the agent channel (empty = silent). When it's removed,
  what is the explicit set of event kinds the mcp channel forwards? Probably
  terminal lifecycle + external (woken) events, but this should be enumerated.

- **`IsTerminal` boundary.** Does the notify daemon care about AUTO_ARCHIVED, or
  only the run-ending transitions (SANDBOX_EXITED→Completed/Failed,
  SANDBOX_FAILED, CANCELLED)? The issue says "stopped / completing / erroring",
  which is the latter.

- **Migration / rollout.** `Event` and `ChannelMessage` can coexist for one
  release (attach `Event`, keep `ChannelMessage`) so older `xagent` clients
  keep working while consumers move over, then `ChannelMessage` is dropped. Is a
  deprecation window wanted, or is a single atomic change acceptable given
  server and CLI ship together?

- **Notify auth/scope.** Out of scope here, but the daemon subscribes to the
  whole org stream; per-user or per-task scoping (cf. #802) may interact with
  which events it should surface.
