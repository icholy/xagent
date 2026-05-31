# Explicit per-task subscriptions for channel notifications

Issue: https://github.com/icholy/xagent/issues/793

## Problem

The Claude Code channel feed pushes a notification to the receiving agent for
**every** channel-worthy state change in the agent's org — task created, queued,
woken by event, completed, failed, cancelled. An agent that is actively driving
work (the orchestrator pattern: spawning and supervising many tasks) gets pulled
off its current thread to acknowledge or react to each one, instead of staying
focused. The per-event, interrupt-style delivery is the exact distraction the
feed was meant to prevent.

These messages are meant as *situational awareness*, not action items, but their
volume and timing make them behave like interrupts.

### How channel notifications reach an agent today

The push half lives entirely in the local stdio bridge, `xagent mcp --channel`
(`internal/command/mcp.go`):

1. The bridge opens an SSE subscription to the C2 server's **per-org** stream
   (`GET /events`) via `xagentclient.NewNotificationClient`.
2. For every `model.Notification` that arrives, the handler forwards it to the
   host Claude Code session as a `notifications/claude/channel` event **iff**
   `n.ChannelMessage != ""`:

   ```go
   Handler: func(n model.Notification) {
       if n.ChannelMessage == "" {
           return
       }
       if err := transport.SendChannel(ctx, mcpchannel.Params{Content: n.ChannelMessage}); err != nil {
           slog.Warn("xagent channel: failed to send", "error", err)
       }
   },
   ```

The only two filters that exist today are:

- **`ChannelMessage != ""`** — the summary gate from
  `proposals/implemented/summary-gated-channel-notifications.md`. It already
  suppresses log spam and lifecycle churn, leaving only terminal-status and
  queued-for-runner transitions.
- **own-`ClientID` suppression** — `NotificationClient` drops notifications
  stamped with the bridge's own client id so the agent doesn't echo its own
  `create_task` / `update_task` mutations (`notificationclient.go:140`).

Neither filter is scoped to *which tasks the agent cares about*. Every
terminal/queued transition for **any** task in the org — including the dozens of
tasks an orchestrator spawned hours ago and is no longer waiting on — is
forwarded. That is the firehose #793 describes.

This proposal is scoped to that bridge. In-container agents (`internal/agentmcp`)
do not consume channels yet — that remains future work in
`proposals/implemented/claude-code-channels.md`. When they do, they reuse this
same `xagent mcp --channel` code path and inherit this mechanism unchanged.

## Design

Add an explicit, opt-in, per-task subscription registry to the bridge process,
and gate channel forwarding on it.

- **Mute by default.** With no subscriptions, the agent receives **zero** channel
  interrupts. The `ChannelMessage` gate still applies on top, so even a subscribed
  task only surfaces the events the summary gate already deems channel-worthy.
- **Explicit opt-in via MCP tools.** The agent calls `watch_task(task_id)` for
  each task it wants situational awareness on, and `unwatch_task(task_id)` when it
  stops caring.
- **No auto-subscribe.** Creating a task does **not** watch it. An orchestrator
  that spawns 20 fire-and-forget tasks gets no interrupts from them unless it
  explicitly opts in — which is the whole point.

### Where subscriptions live: the bridge process

Subscriptions are **in-memory, per-bridge-process state**, not server state. This
mirrors where the two existing channel filters already live (the `ChannelMessage`
gate and own-`ClientID` suppression are both applied client-side in the bridge),
and follows the precedent set in the summary-gated proposal: *"Notification
batching… belongs to the bridge, not to `model.Notification`."* Filtering is the
same kind of concern.

Concretely, a small concurrency-safe set:

```go
// watchset is the set of task ids the bridge forwards channel
// notifications for. Empty means mute-by-default: nothing is forwarded.
// It is mutated by the watch_task / unwatch_task tools (MCP request
// goroutines) and read by the SSE notification handler goroutine, so all
// access goes through the mutex.
type watchset struct {
    mu  sync.Mutex
    ids map[int64]struct{}
}

func newWatchset() *watchset { return &watchset{ids: map[int64]struct{}{}} }

func (w *watchset) add(id int64) { w.mu.Lock(); defer w.mu.Unlock(); w.ids[id] = struct{}{} }
func (w *watchset) remove(id int64) { w.mu.Lock(); defer w.mu.Unlock(); delete(w.ids, id) }

func (w *watchset) has(id int64) bool {
    w.mu.Lock()
    defer w.mu.Unlock()
    _, ok := w.ids[id]
    return ok
}

func (w *watchset) list() []int64 {
    w.mu.Lock()
    defer w.mu.Unlock()
    ids := make([]int64, 0, len(w.ids))
    for id := range w.ids {
        ids = append(ids, id)
    }
    slices.Sort(ids)
    return ids
}
```

The set is created once per `xagent mcp` invocation, alongside the existing
`clientID`.

### The forwarding gate

The notification handler grows one more condition: the notification must name a
watched task. Task ids ride along in `n.Resources` (each
`NotificationResource` carries `Type: "task"` and an `ID`), which the current
handler ignores entirely. We extract the primary task id and check membership:

```go
// primaryTaskID returns the id of the first task resource in the
// notification, if any.
func primaryTaskID(n model.Notification) (int64, bool) {
    for _, r := range n.Resources {
        if r.Type == "task" {
            return r.ID, true
        }
    }
    return 0, false
}
```

```go
Handler: func(n model.Notification) {
    if n.ChannelMessage == "" {
        return // summary gate: not channel-worthy
    }
    id, ok := primaryTaskID(n)
    if !ok || !watch.has(id) {
        return // mute-by-default: not a task this agent is watching
    }
    if err := transport.SendChannel(ctx, mcpchannel.Params{
        Content: n.ChannelMessage,
        Meta:    map[string]string{"resource": "task", "id": strconv.FormatInt(id, 10)},
    }); err != nil {
        slog.Warn("xagent channel: failed to send", "error", err)
    }
},
```

Two things change beyond the gate:

- A notification with `ChannelMessage` set but **no** task resource is now
  dropped (mute-by-default has nothing to match). The publishing-site survey in
  `summary-gated-channel-notifications.md` shows every `ChannelMessage` is set at
  a site that has a task in scope, so this drops nothing in practice; it is the
  correct default for any future message that lacks a task.
- We now populate channel `meta` with the task id (`resource`/`id`) so the model
  can call `get_task` for the full payload — the summary-gated proposal specified
  this but the shipped handler omitted it. Folding it in here is a natural
  cleanup since we already have the id in hand.

### MCP tool surface

Three tools, registered on the bridge's MCP server **only when `--channel` is
enabled** (without channels there is nothing to subscribe to). They are bridge
tools, not server-proxy tools: they mutate the local `watchset` and do not call
the C2 RPC API, so they live in the bridge alongside the watchset rather than in
`mcpserver.AddTools` (whose handlers all proxy to the Connect service).

| Tool | Input | Behaviour |
| --- | --- | --- |
| `watch_task` | `task_id int64` | Add `task_id` to the watch set. Idempotent. The agent now receives this task's channel-worthy transitions. |
| `unwatch_task` | `task_id int64` | Remove `task_id`. Idempotent. No further channel events for it. |
| `list_watched_tasks` | — | Return the sorted set of currently-watched task ids, so the agent can introspect its own subscriptions. |

Sketch (in `internal/command/mcp.go`, gated by `--channel`):

```go
func addWatchTools(server *mcp.Server, watch *watchset) {
    mcp.AddTool(server, &mcp.Tool{
        Name: "watch_task",
        Description: "Receive Claude Code channel notifications for a task's " +
            "status changes (queued, woken, completed, failed, cancelled). " +
            "Channel notifications are muted by default — call this for each " +
            "task you want situational awareness on. Distinct from " +
            "create_link(subscribe=true), which routes external events to a task.",
    }, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
        TaskID int64 `json:"task_id" jsonschema:"The task ID to watch"`
    }) (*mcp.CallToolResult, any, error) {
        watch.add(in.TaskID)
        return jsonResult(map[string]any{"watching": watch.list()}), nil, nil
    })
    // unwatch_task and list_watched_tasks follow the same shape.
}
```

`watch_task` is a pure local set insertion — it deliberately does **not**
round-trip to the server to validate the task exists. Watching a non-existent or
not-yet-created id is harmless (no notification will ever match it), and keeping
the tool offline avoids both an RPC per call and a failure mode where a transient
server error blocks the agent from muting itself. (See Open Questions for the
validate-on-watch alternative.)

### Naming: why not `subscribe` / `unsubscribe`

The codebase already has a "subscribe" concept on a **different axis**:
`create_link(subscribe=true)` (`internal/server/apiserver/link.go`) marks a *link*
so that external events whose URL matches the link are **routed to the task** by
the event router. That is server-side, persistent, and about *inbound* event
delivery to a task.

This feature is the inverse direction: it controls whether *outbound* channel
notifications about a task reach *this agent's session*. It is client-side,
ephemeral, and per-agent. Calling both "subscribe" would conflate two unrelated
mechanisms in the agent's tool list. `watch_task` / `unwatch_task` reads
naturally against "mute by default" (you *watch* the tasks you care about) and
keeps the two concepts cleanly separated. The tool descriptions call out the
distinction explicitly.

### Lifecycle and persistence

- **Ephemeral by design.** The watch set lives only for the bridge process /
  Claude Code session. A restarted session starts muted and re-watches what it
  cares about. This is correct: stale subscriptions from a previous session
  should not silently resurrect as interrupts.
- **No schema, no proto, no migration.** Nothing is stored server-side. The C2
  server, the runner consumer, the web UI consumer, and `model.Notification` are
  all untouched. The org-wide SSE stream still carries every notification to the
  bridge; the bridge filters client-side, exactly as it already does for
  `ChannelMessage` and own-`ClientID`.

## Trade-offs

### Client-side (bridge) filtering vs. server-side subscription state

We filter in the bridge. The alternative is to push the watch set to the server
(a `Subscribe`/`Unsubscribe` RPC plus per-client subscription state and a
server-side filter on the SSE fan-out).

- **Client-side (chosen):** zero server changes, no proto/migration, no new
  failure modes, and it sits next to the two filters already implemented
  client-side. The cost is that the bridge still receives the full org firehose
  over the wire and discards most of it — but that traffic already flows today
  (the web UI is a consumer of the same stream) and is trivial.
- **Server-side:** would save the wire traffic and could enforce scoping
  centrally, but introduces persistent per-client state on the server, a new RPC
  surface, and reconnection/resync complexity (the SSE stream already reconnects
  with backoff; a server-held subscription set would need to survive or be
  re-sent across reconnects). Not justified for a low-volume notification stream.

If bandwidth or cross-tenant secrecy ever became a concern, server-side filtering
could be added later without changing the agent-facing tool contract.

### Mute-by-default vs. auto-subscribe-on-create

The decided direction is mute-by-default with no auto-subscribe, and this
proposal implements exactly that. Auto-subscribing on `create_task` was
explicitly rejected: an orchestrator that spawns many tasks would immediately be
back in the firehose for all of them. Requiring an explicit `watch_task` keeps
the agent's attention surface equal to the set of tasks it has consciously
decided to wait on.

The minor cost is one extra tool call after `create_task` for tasks the agent
*does* want to track. That is acceptable — and is the lever that lets a
fire-and-forget spawn stay silent.

### `watch_task` / `unwatch_task` vs. a single `set_watched_tasks(ids)`

A single "replace the whole set" tool would be more declarative and idempotent in
one shot. We chose incremental add/remove because the orchestrator's mental model
is incremental ("I just created task 91, start watching it"; "task 88 is done,
stop watching it"), and a replace-the-set call forces the agent to first recall
the full current set (or call `list_watched_tasks`) before every mutation.
`list_watched_tasks` covers the introspection need without that coupling.

## Open Questions

1. **Auto-unwatch on terminal status.** When a watched task reaches a terminal
   state, the agent gets its `"Task N completed."` notification — exactly what it
   was waiting for — and then almost never wants further events about that task
   (e.g. a later auto-archive transition, were one ever channel-worthy). Should
   the handler auto-remove a task from the watch set *after* forwarding a terminal
   notification, so the agent doesn't have to call `unwatch_task` itself?
   Recommendation: yes — it matches intent and prevents slow leak of stale
   subscriptions, and the agent can always re-watch. Flagged because it adds a
   small amount of status-awareness to the otherwise dumb forwarding gate (the
   handler would need to recognise terminal `ChannelMessage`s, or
   `model.Notification` would need a cheap terminal flag).

2. **Validate-on-watch.** Should `watch_task` round-trip to the server to confirm
   the task exists (and belongs to the agent's org) and return an error
   otherwise? Pro: catches typos early. Con: an RPC per call and a new failure
   mode. Recommendation: no — keep it a local no-op insert; watching a bad id is
   inert.

3. **Cap on watch-set size.** Should the bridge cap the number of simultaneously
   watched tasks to bound notification volume even when an agent over-subscribes?
   Probably unnecessary given mute-by-default already makes over-subscription a
   deliberate act, but flagged.

4. **Surfacing the mute default to the agent.** The agent needs to *know* it is
   muted by default, or it will wonder why completions aren't arriving. The
   `xagent mcp` server `Instructions` (and/or the orchestrator skill) should state
   the rule: "channel notifications are muted by default; call `watch_task(id)`
   for each task you want to be notified about." Where to put this — server
   instructions, tool descriptions, or the orchestrator skill — is an
   implementation detail to settle.
