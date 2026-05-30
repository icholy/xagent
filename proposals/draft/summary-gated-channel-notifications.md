# Summary-Gated Channel Notifications

## Problem

`model.Notification` (`internal/model/notification.go`) is a shared change bus published by `apiserver.Server.publish` (`internal/server/apiserver/apiserver.go:54`) and fanned out to three very different consumers:

1. **Runners.** They watch `Notification.Runner` (`for_runner`) to learn which runner has pending work to claim. They don't care about the resources at all.
2. **Web UI** (TanStack Query, `webui/src/lib/notification-sse.ts`). It inspects `Resources: [{action, type, id}]` to know which queries to invalidate and refetch. The thin envelope is exactly what it wants — the FE re-fetches the canonical resource from the API, it doesn't try to render the notification itself.
3. **Agent channel events.** The local `xagent mcp` bridge (`internal/command/mcp.go:86`) subscribes to the SSE stream and pushes task changes into the host Claude Code session as `notifications/claude/channel` events via `mcpserver.ForwardNotification` (`internal/server/mcpserver/channel.go:21`).

Consumer (3) is the broken one. `ForwardNotification` today:

```go
func ForwardNotification(ctx context.Context, sender ChannelSender, n model.Notification) error {
    if n.Type != "change" {
        return nil
    }
    var errs []error
    for _, r := range n.Resources {
        if r.Type != "task" {
            continue
        }
        errs = append(errs, sender.SendChannel(ctx, mcpchannel.Params{
            Content: fmt.Sprintf("task %d was %s.", r.ID, r.Action),
            Meta: map[string]string{
                "resource": "task",
                "id":       strconv.FormatInt(r.ID, 10),
            },
        }))
    }
    return errors.Join(errs...)
}
```

Three problems with that:

- **Lossy verbs.** Every task-mutating site emits `Action: "updated"` (or `"created"` / `"archived"` / `"cancelled"` / `"restarted"`), but a runner event that transitions a task to `RUNNING`, a runner event that transitions it to `COMPLETED`, and an instruction being appended all collapse into the same string: `task N was updated.` The Claude session can't distinguish them.
- **Dropped resources.** `r.Type != "task"` discards every event-, link-, and log-typed resource. The interesting wake case — `AddEventTask` (`internal/server/apiserver/event.go:111`), where a GitHub PR comment or Jira webhook routes through `eventrouter` and links itself onto a subscribed task — is conveyed by `[{type:"task",action:"updated"}, {type:"event",action:"updated"}]`. The channel session sees `task N was updated.` with no hint *why*.
- **Context is gone by the time the bridge sees it.** The semantic context that would make the notification useful (which status the task moved to, which event woke it, what changed in `UpdateTask`, which runner event arrived) exists only at the `publish()` call site. By the time the bridge receives the SSE frame, all it has is `(action, type, id)`. Reconstructing the *cause* from the post-state by fetching the task is impossible: a task observed in `RUNNING` could have just started, or could have been running for an hour while a log line was appended.

We need to enrich the channel-facing output without disturbing consumers (1) and (2), who are perfectly happy with the current envelope.

## Design

### A single optional field

Add one field to `model.Notification`:

```go
type Notification struct {
    Type      string                 `json:"type"`
    Resources []NotificationResource `json:"resources,omitempty"`
    Time      time.Time              `json:"timestamp"`
    OrgID     int64                  `json:"org_id"`
    UserID    string                 `json:"user_id,omitempty"`
    ClientID  string                 `json:"client_id,omitempty"`
    Runner    string                 `json:"for_runner,omitempty"`
    Summary   string                 `json:"summary,omitempty"` // NEW
}
```

`Summary` is free-text, human-readable, and authored by the publishing call site from the context already local to it. It is intentionally *not* structured: the channel content is rendered into a `<channel>` tag in Claude's context, and a sentence is what the model can act on.

### Summary as the channel gate

`ForwardNotification` becomes a pure relay:

```go
func ForwardNotification(ctx context.Context, sender ChannelSender, n model.Notification) error {
    if n.Type != "change" || n.Summary == "" {
        return nil
    }
    meta := map[string]string{}
    if id, ok := primaryTaskID(n.Resources); ok {
        meta["resource"] = "task"
        meta["id"] = strconv.FormatInt(id, 10)
    }
    return sender.SendChannel(ctx, mcpchannel.Params{
        Content: n.Summary,
        Meta:    meta,
    })
}
```

Key consequences:

- **`Summary == ""` ⇒ no channel event.** The bridge stays silent. This is how we drop lifecycle churn (key created, workspace registered, log line appended, link created by the agent itself) that would otherwise spam the Claude session.
- **One frame per notification, not one per resource.** The fan-out-over-resources loop disappears. A single `Resources: [{task, updated}, {event, updated}]` produces one well-formed sentence, not two confusing ones.
- **The `type != "task"` filter is gone.** Whether resources contain a task, an event, both, or neither, the gate is `Summary != ""`. Resource info is still carried to populate `meta` attributes (so `get_task` from the model can still find an `id` to query), but resource presence is no longer the trigger.
- **The `fmt.Sprintf` templating and per-resource `action` rendering are gone.** No more `task N was updated.` There is no fallback string for cases where a call site forgot a summary — absence of `Summary` is interpreted as *intentional* silence, not "render the default."

### Backward compatibility

- **Runner consumer:** unaffected. It reads `Runner` and ignores everything else.
- **Web UI consumer:** unaffected. It reads `Resources` and ignores everything else. JSON parsing tolerates the new optional field; existing FE never deserializes `summary` so the existing types don't even need to be updated unless someone wants to use it.
- **SSE wire format:** purely additive (`summary` is `omitempty`). Old SSE clients keep working.
- **No migration:** the field is in-memory and on the SSE wire; nothing in PostgreSQL changes.

The FE *could* later choose to surface summaries (e.g. as toast notifications, or in an activity feed); that's out of scope here. The point is the field is freely available to it if it ever wants it.

### Per-site summary table

The proposal is to set `Summary` at every existing `s.publish(model.Notification{...})` call site. Sites left with an empty summary are intentional — they represent cleanup, housekeeping, or self-traffic that wouldn't be agent-actionable.

Confirmed list (from `grep -rn "s\.publish(" internal/server/apiserver/`):

| File:line | Handler | Proposed `Summary` |
| --- | --- | --- |
| `task.go:120` | `CreateTask` | `fmt.Sprintf("Task %d created (%s/%s).", task.ID, task.Runner, task.Workspace)` |
| `task.go:230` | `UpdateTask` | `fmt.Sprintf("Task %d updated: %s.", task.ID, strings.Join(changed, ", "))` — uses the existing `changed` slice; if `req.Start` was true include the resulting `task.Status` (e.g. `"Task 42 updated: status (now PENDING), instructions"`) |
| `task.go:275` | `ArchiveTask` | `fmt.Sprintf("Task %d archived.", task.ID)` |
| `task.go:320` | `UnarchiveTask` | `fmt.Sprintf("Task %d unarchived.", task.ID)` |
| `task.go:365` | `CancelTask` | `fmt.Sprintf("Task %d cancellation requested (status %s).", task.ID, task.Status)` |
| `task.go:410` | `RestartTask` | `fmt.Sprintf("Task %d restart requested.", task.ID)` |
| `event.go:45` | `CreateEvent` | `""` — an event existing isn't agent-actionable until it's routed to a task. Stays silent. |
| `event.go:78` | `DeleteEvent` | `""` — cleanup. |
| `event.go:111` | `AddEventTask` | **the motivating case — see below.** |
| `event.go:147` | `RemoveEventTask` | `""` — bookkeeping. |
| `link.go:36` | `CreateLink` | `""` — agents create their own links via the in-container MCP server; pinging the channel about a link the agent just created is a self-echo (the bridge's ClientID filter would already drop most of these, but suppressing at emission is cleaner and covers links created from the FE too). |
| `log.go:31` | `UploadLogs` | `""` — log lines fire constantly during a running task; pushing one channel event per log batch would drown the session. The FE/UI use this; the agent does not need it. |
| `org.go:108` | `AddOrgMember` | `""` — org admin housekeeping. |
| `org.go:143` | `RemoveOrgMember` | `""` — org admin housekeeping. |
| `key.go:36` | `CreateKey` | `""` — API key management is out-of-band. |
| `key.go:67` | `DeleteKey` | `""` — same. |
| `workspace.go:31` | `RegisterWorkspaces` | `""` — runner startup. |
| `workspace.go:64` | `ClearWorkspaces` | `""` — runner shutdown / reset. |
| `runner.go:56` | `SubmitRunnerEvents` | Switch on the resulting `task.Status` (which is in scope after `ApplyRunnerEvent`): `"Task %d is now running."` / `"Task %d completed."` / `"Task %d failed."` / `"Task %d cancelled."` / `"Task %d is restarting."` Use the post-state because that's what's agent-actionable; the raw runner event (`started`/`stopped`/`failed`) doesn't carry the same meaning by itself (e.g. `stopped` can mean completed, restarting, or cancelled depending on the task's pending command). |

That's the entire surface — every site is accounted for, and the silent ones are silent by deliberate choice, not by oversight.

### The motivating case: `AddEventTask` (the wake)

`AddEventTask` (`internal/server/apiserver/event.go:89-122`) is invoked by `eventrouter` (`internal/eventrouter/eventrouter.go`) when an external event — a GitHub PR comment, a Jira webhook, etc. — matches a `subscribe=true` link on a task. The handler has both `req.TaskId` and `req.EventId` in scope. Today its notification is indistinguishable from any other `task updated` event; the agent has no way to know it was woken, let alone by what.

The summary needs to convey *what woke the task*, not just *that something happened*. The event row carries a `Description` (`internal/model/event.go:13`, populated by the routing source — for example, the GitHub poller writes things like `"PR comment from alice: please rebase"`). Loading it is one DB hit:

```go
ev, err := s.store.GetEvent(ctx, nil, req.EventId, caller.OrgID)
// ev.Description is the human-meaningful cause
```

Proposed summary:

```go
summary := fmt.Sprintf("Task %d woken by event %d: %s", req.TaskId, req.EventId, ev.Description)
if ev.URL != "" {
    summary += " (" + ev.URL + ")"
}
```

Rendered example (PR comment routed to a task):

```
<channel source="xagent" resource="task" id="42">
Task 42 woken by event 17: PR comment from alice on icholy/xagent#481: please rebase (https://github.com/icholy/xagent/pull/481#issuecomment-...)
</channel>
```

That sentence, plus the embedded URL, is enough for Claude in the local session to decide whether to immediately fetch the PR, call `get_task`, or wait. The agent now reacts to the *cause*, not the side effect.

The extra `GetEvent` round-trip costs one indexed lookup per event-routing operation. Event routing is bounded by external webhook rate (low frequency vs. log appends), so the cost is acceptable; the alternative (passing the description through `AddEventTaskRequest` as a denormalized field) leaks routing concerns into the proto.

If the `GetEvent` call fails, the handler should still publish — fall back to `fmt.Sprintf("Task %d woken by event %d.", req.TaskId, req.EventId)` rather than logging an error and emitting nothing. Losing the description is acceptable; losing the wake notification is not.

### What stays out

- **Channel `meta`.** The current `meta` map (`resource`, `id`) stays for task notifications so the model can still find a `task_id` to call `get_task` with. We do *not* add a `summary` meta — the summary is the body.
- **Resource fan-out.** The forwarder no longer emits one event per resource. One notification = one channel event. Multi-resource notifications (task + log, task + event) are summarized as a single sentence by the call site that knows which resource is primary.
- **Notification batching.** Out of scope. If channel-event volume becomes a problem in practice, batching belongs to the bridge, not to `model.Notification`.

## Trade-offs

### Free-text `Summary` vs. structured `Kind` + typed fields

We could replace `Summary string` with something like:

```go
type Notification struct {
    // ...
    Kind  string         `json:"kind,omitempty"`   // "task_started", "task_woken_by_event", ...
    Cause map[string]any `json:"cause,omitempty"`  // typed payload per Kind
}
```

The bridge would then look up a template per `Kind` and render the sentence client-side.

Rejected because:

- **The renderer would live in the bridge,** not in the C2 server. Adding a new event kind would mean updating two places (publish site + renderer table). Today's `ForwardNotification` is a pure relay; the kind table would re-introduce the same coupling we're trying to eliminate.
- **Channel content is for humans (well, an LLM that reads English).** Structuring the payload only to turn it back into English at the edge is busywork. The publishing site already has the raw values formatted in its log lines; it's the natural place to write the sentence.
- **A free-text field doesn't preclude structure later.** If a future consumer needs `Kind`-style routing, it can be added additively; the summary becomes the human-readable fallback.

### Enriching at the channel boundary (fetch on receipt)

The bridge could, on every received notification, fetch the affected task/event/link and render a sentence from the fresh state. Considered and rejected because:

- **Race-y.** By the time the bridge fetches, the task may have transitioned again. The fetched state isn't necessarily the state that triggered the notification.
- **Loses the cause.** A task in `RUNNING` could have just started, or could have been running while a log line appended. The boundary cannot distinguish them.
- **Costs an RPC per notification.** Channel events fire frequently; an extra round-trip per event scales poorly.
- **Re-reads what we already had.** The publish site computed `changed []string`, decided the new status, and knows which event id was just attached. Throwing that away and re-reading it from disk is wasteful.

The publishing site is the only place where the cause is unambiguously known; the summary belongs there.

### Channel gate vs. always-emit-with-fallback

We could have `ForwardNotification` fall back to the existing `task N was updated.` template when `Summary == ""`. Rejected:

- **It defeats the silencing.** The whole reason `UploadLogs` and `RegisterWorkspaces` set `""` is that we *don't* want a channel event for them. A fallback would re-create today's noise.
- **It hides bugs.** If a call site forgets to set `Summary`, we want the agent to notice the missing event (and us to fix it) rather than papering over with a useless string.

Absence of `Summary` is part of the contract: it means "this is silent on the channel."

## Test plan

The change is small enough to cover with a handful of focused tests.

**`internal/server/mcpserver/channel_test.go`** — replace the existing `TestForwardNotification` (which asserts the lossy template) with:

- `TestForwardNotification_GatesOnEmptySummary` — feed a `Notification{Type: "change", Resources: [...], Summary: ""}`, assert `sender.calls == 0`.
- `TestForwardNotification_RelaysSummary` — feed `Summary: "Task 1 is now running."`, assert exactly one `SendChannel` call with that content and `meta` populated from the first task resource.
- `TestForwardNotification_NoTaskResource` — feed a notification whose `Resources` contain no task entries but `Summary` is set, assert one call with no `id` in `meta`. (Confirms the gate is the summary, not the resource type.)
- `TestForwardNotification_IgnoresNonChange` — feed `Type: "ready"`, assert zero calls.

**`internal/server/apiserver/publish_test.go`** — extend the existing publish tests with one representative emission-site assertion:

- `TestAddEventTask_SummaryNamesEvent` — create a task and an event with a known description, call `AddEventTask`, capture the published notification from the test publisher, assert the `Summary` field contains both the task id and the event description.
- `TestUpdateTask_SummaryListsChanges` — call `UpdateTask` with `Start: true` and an added instruction, assert the summary includes `"status"` and `"instructions"`.
- `TestUploadLogs_NoSummary` — call `UploadLogs`, assert the published notification has `Summary == ""` (regression guard for the silencing decision).

No new test infrastructure is needed; the in-memory `pubsub` publisher (`internal/pubsub/local.go`) already captures published notifications for inspection.

## Open Questions

- **Locale / wording.** The summaries are written in English. The codebase has no i18n elsewhere; not raising this as a blocker, noting it for completeness.
- **Truncation.** Should `ForwardNotification` cap summary length (e.g. 500 chars) to defend against a runaway event description? Probably yes — a one-line guard in the forwarder is cheap insurance. Default cap to be decided in implementation.
- **FE consumption.** Whether the FE should also surface `Summary` (e.g. as a toast title) is out of scope; flagged so the implementer doesn't accidentally remove the field from the FE-side TypeScript types.
