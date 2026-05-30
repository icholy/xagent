# Claude Code Channels for a local xagent MCP bridge

Issue: https://github.com/icholy/xagent/issues/466

## Problem

A common way to drive xagent is from a local Claude Code session: a developer runs `claude` on their workstation, and that session creates and supervises xagent tasks through xagent's user-facing MCP server. Today, after creating a task, the local Claude has no way to know when something changes — it must poll `get_task` or `list_tasks` to discover new logs, new instructions, status transitions, or completion. Polling wastes turns, delays reactions, and bloats the model's context with repeated reads.

Claude Code Channels (research preview, v2.1.80+) provide exactly the primitive that's missing: an MCP server can push `notifications/claude/channel` events into a running session as `<channel>` tags in Claude's context, so the model reacts on the next turn without polling. The C2 server already publishes structured change notifications for every task mutation; the gap is the transport that delivers them to the local Claude.

## Background: How Channels Work

Sources: [code.claude.com/docs/en/channels](https://code.claude.com/docs/en/channels), [code.claude.com/docs/en/channels-reference](https://code.claude.com/docs/en/channels-reference).

- **Status**: research preview. Requires Claude Code **v2.1.80+** (one-way + tools); permission relay needs **v2.1.81+**.
- **Capability declaration**: an MCP server registers as a channel by setting `capabilities.experimental["claude/channel"] = {}`. The value is always an empty object — its presence is the signal. Two-way channels additionally declare `tools: {}`; permission relay adds `capabilities.experimental["claude/channel/permission"] = {}`.
- **Notification format**: method `notifications/claude/channel`, params `{ content: string, meta: Record<string, string> }`. `content` becomes the body of a `<channel>` tag; each `meta` entry becomes a tag attribute. The `source` attribute is auto-populated from the server's configured name.
- **`meta` keys must be identifiers**: letters, digits, and underscores only. Keys containing hyphens or other characters are **silently dropped**. Use `task_id`, not `task-id`.
- **Transport is stdio-only**: a channel server must be a subprocess spawned by Claude Code. Streamable HTTP MCP servers cannot register as channels.
- **Delivery is fire-and-forget**: the notification call resolves when the JSON-RPC frame is written to the transport, not when Claude processes it. If the session didn't load the server with `--channels`, or org policy blocks channels, events are dropped silently with no error returned. Guaranteed delivery requires a reply tool that the model can call back through.
- **Queuing**: events are delivered in order, and multiple events arriving while Claude is mid-turn are batched onto the next turn.
- **Allowlist constraint**: during the preview, `--channels` only accepts plugins on an Anthropic-curated allowlist. A custom server like `xagent` is not on it, so the session must launch with `--dangerously-load-development-channels server:xagent` (which prompts for confirmation per entry), or the org must add the server to the `allowedChannelPlugins` managed setting. Being listed in `.mcp.json` is necessary but not sufficient — the server must also be named in `--channels`.
- **Auth/platform constraints**: channels require Anthropic auth via claude.ai or a Console API key. They are not available on Amazon Bedrock, Google Vertex AI, or Microsoft Foundry. Team/Enterprise orgs must enable `channelsEnabled` in managed settings.
- **Channels are a notification layer, not a data layer**. The event says "task 42 updated" with small `meta` attributes; Claude then calls `get_task` for the full payload.

An event arrives in Claude's context as:

```
<channel source="xagent" action="updated" resource="task" id="42">
Task 42 was updated.
</channel>
```

## Design

### Two MCP servers already exist

The proposal hinges on distinguishing the two xagent MCP servers in the tree today:

1. **User-facing MCP server** (`internal/server/mcpserver/mcpserver.go`, backed by package `mcpserver`). Served as MCP **Streamable HTTP** via `mcp.NewStreamableHTTPHandler` with `Stateless: true`, mounted on the C2 HTTP API at `/mcp`. Exposes `list_workspaces`, `create_task`, `get_task`, `list_tasks`, `update_task`. This is what the developer's local Claude Code talks to today.

2. **In-container agent MCP server** (`internal/command/mcp.go` — `McpCommand` — backed by `internal/agentmcp`). stdio transport. Spawned by the runner inside each task's container. Exposes `get_my_task`, `update_my_task`, `report`, `create_link`, and the child-task tools. (A separate task is moving this command out of the top-level `mcp` slot to `xagent tool agent-mcp`, freeing `xagent mcp` for the new bridge described below.)

This proposal only affects path (1): how the local Claude that drives the user-facing server receives push notifications. Pushing into in-container agents is explicitly out of scope (see "Future work").

### The hard constraint

The user-facing server is stateless Streamable HTTP. **Channels require stdio.** We cannot simply add `claude/channel` to the experimental capabilities in `mcpserver.go` and have it push notifications into a session — the session does not have a long-lived bidirectional connection to that handler, and `--channels` does not accept HTTP MCP servers. Any push delivery has to happen over a stdio subprocess that Claude Code spawns.

### The bridge

Introduce a new top-level subcommand:

```
xagent mcp [--server URL] [--token TOKEN]
```

A local stdio MCP server that the developer's `.mcp.json` launches. It does two things:

1. **Re-exposes the user-facing tools** (`list_workspaces`, `create_task`, `get_task`, `list_tasks`, `update_task`) over stdio, proxying each call to the C2 server via `xagentclient.New(...)` (the existing Connect RPC client). For a CLI-driven setup this replaces the remote HTTP MCP entry, so the developer only needs **one** MCP entry instead of an HTTP endpoint plus a separate channel process.

2. **Declares the `claude/channel` capability** and pushes `notifications/claude/channel` events for task changes by translating an SSE subscription to the existing notification stream.

The user-facing HTTP MCP endpoint at `/mcp` stays in place for hosted/web-driven Claude clients that cannot spawn local subprocesses.

#### Updated architecture

```
Local Claude Code session
    ↕ stdio (MCP tools + notifications/claude/channel)
xagent mcp  (NEW local bridge — proxies tools, translates SSE → channel)
    ↕ HTTP: Connect RPC (tools)  +  SSE subscription (notifyserver)
xagent C2 server  (already publishes task notifications on every change)
```

### Reusing the existing notification pipeline

This is a translator, not a new event system. The pieces are already in place:

- `internal/server/apiserver/apiserver.go` calls `s.publish(model.Notification{...})` on every mutating RPC (`task.go`, `event.go`, `log.go`, `link.go`, `workspace.go`, `key.go`, `org.go`, `runner.go`). Every task create / update / status change / log append / link append already produces a notification.
- `internal/server/notifyserver/sse.go` fans these notifications out per-org over an SSE endpoint mounted at `/events`. The web UI is already a consumer of this stream for live updates.
- `internal/model/notification.go` defines the payload:

  ```go
  type Notification struct {
      Type      string                 `json:"type"`       // "ready" | "change"
      Resources []NotificationResource `json:"resources,omitempty"`
      Time      time.Time              `json:"timestamp"`
      OrgID     int64                  `json:"org_id"`
      UserID    string                 `json:"user_id,omitempty"`
      ClientID  string                 `json:"client_id,omitempty"`
      Runner    string                 `json:"for_runner,omitempty"`
  }

  type NotificationResource struct {
      Action string `json:"action"` // created | updated | appended
      Type   string `json:"type"`   // task | event | log | link | task_logs
      ID     int64  `json:"id"`
  }
  ```

  Every field that ends up in a channel `meta` attribute (`action`, `type`, `id`) is already identifier-safe — letters, digits, underscores only — so they pass the channel `meta` key/value rules without transformation.
- `internal/x/sse` is an existing SSE Reader/Writer the bridge can consume.

The bridge:

1. Connects to the C2 SSE endpoint (`GET /events`, `Accept: text/event-stream`) using the same auth token configured for the RPC client.
2. Reads `model.Notification` JSON payloads via `internal/x/sse.Reader`.
3. Filters down to task-relevant resources (`type` in `{task, log, link, task_logs, event}`).
4. For each surviving `NotificationResource`, emits one `notifications/claude/channel`:

   ```jsonc
   {
     "method": "notifications/claude/channel",
     "params": {
       "content": "Task 42 was updated.",
       "meta": {
         "action":   "updated",
         "resource": "task",
         "id":       "42"
       }
     }
   }
   ```

   Channel `meta` requires identifier keys, so we rename `Type` → `resource` (since `type` is also reserved in some contexts) and stringify `ID`. `OrgID`, `UserID`, `ClientID` are not forwarded to the model.

5. Reconnects the SSE stream on transport errors with backoff, mirroring the web UI's behavior.

The bridge does **not** open new RPCs or read full task payloads. Claude does that itself by calling `get_task` through the same bridge after the channel event arrives.

### `mcpserver.AddTools` refactor

To avoid duplicating the tool schemas between the HTTP handler and the new stdio bridge, extract the tool registrations currently inline in `mcpserver.Server.Handler()` (the five `mcp.AddTool(server, ...)` calls plus the input/output types) into a reusable function on the `mcpserver` package, roughly:

```go
// AddTools registers the user-facing xagent tools on the given MCP server.
// Both the HTTP handler and the local stdio bridge call this so they share
// schemas, descriptions, and behavior.
func AddTools(server *mcp.Server, service xagentv1connect.XAgentServiceHandler, baseURL string) {
    s := &Server{service: service, baseURL: cmp.Or(baseURL, xagentclient.DefaultURL)}
    mcp.AddTool(server, &mcp.Tool{Name: "list_workspaces", /* ... */}, s.listWorkspaces)
    mcp.AddTool(server, &mcp.Tool{Name: "create_task",     /* ... */}, s.createTask)
    mcp.AddTool(server, &mcp.Tool{Name: "get_task",        /* ... */}, s.getTask)
    mcp.AddTool(server, &mcp.Tool{Name: "list_tasks",      /* ... */}, s.listTasks)
    mcp.AddTool(server, &mcp.Tool{Name: "update_task",     /* ... */}, s.updateTask)
}
```

`mcpserver.Handler()` calls `AddTools(server, s.service, s.baseURL)` after constructing the server; the bridge calls the same function with a Connect-client-backed `service` (the existing `xagentclient.Client` type already satisfies the same `XAgentServiceHandler` interface used by `apiserver.Server`, since tool calls just forward to RPCs).

The handler keeps its `Stateless: true` Streamable HTTP wrapper; the bridge wraps the same server with `mcp.StdioTransport` and additionally sets `Capabilities.Experimental["claude/channel"] = map[string]any{}` plus channel-specific `Instructions`.

### `xagent mcp` skeleton

```go
var McpCommand = &cli.Command{
    Name:  "mcp",
    Usage: "Local stdio MCP bridge: re-exposes xagent tools and pushes task change notifications as Claude Code channel events",
    Flags: []cli.Flag{
        &cli.StringFlag{Name: "server", Value: xagentclient.DefaultURL, Usage: "C2 server URL"},
        &cli.StringFlag{Name: "token",  Required: true,                 Usage: "API token"},
    },
    Action: func(ctx context.Context, cmd *cli.Command) error {
        client := xagentclient.New(xagentclient.Options{
            BaseURL: cmd.String("server"),
            Token:   cmd.String("token"),
        })

        server := mcp.NewServer(&mcp.Implementation{
            Name:    "xagent",
            Version: version.String(),
        }, &mcp.ServerOptions{
            Capabilities: &mcp.ServerCapabilities{
                Experimental: map[string]any{
                    "claude/channel": map[string]any{},
                },
            },
            Instructions: "Events from the xagent channel arrive as " +
                "<channel source=\"xagent\" action=... resource=... id=...>. " +
                "They notify you that an xagent task, log, link, or event " +
                "changed. Call get_task with the id for details before acting.",
        })

        mcpserver.AddTools(server, client, cmd.String("server"))

        session, err := server.Connect(ctx, &mcp.StdioTransport{}, nil)
        if err != nil {
            return err
        }
        go pushTaskChannels(ctx, session, client, cmd.String("server"), cmd.String("token"))
        return session.Wait()
    },
}
```

`server.Run` is replaced by the lower-level `server.Connect` + `session.Wait` pair so the bridge can hold a reference to the `ServerSession` and emit notifications from a background goroutine.

### The Go MCP SDK gap (relocated, not eliminated)

The Go MCP SDK v1.2.0 (`github.com/modelcontextprotocol/go-sdk`) supports declaring experimental capabilities via `ServerOptions.Capabilities.Experimental`, but **does not expose a public API to send arbitrary notifications**. The internal `handleNotify` and `ServerSession.getConn()` are unexported, and the public notification methods (`NotifyProgress`, `Log`, `ResourceUpdated`) only cover predefined MCP types — none can send `notifications/claude/channel`.

Resolution options:

1. **Upstream `ServerSession.Notify(ctx context.Context, method string, params any) error`.** The smallest possible change: a public method that delegates to the underlying jsonrpc2 connection's existing `Notify`. Strongly preferred because it unblocks the whole Go MCP ecosystem, not just xagent.
2. **A jsonrpc2-layer wrapper.** The underlying `internal/jsonrpc2.Connection` has a public `Notify(ctx, method, params)`. Build a thin `mcp.Transport` wrapper that retains a reference to the connection before it is handed to the SDK, then call `Notify` directly. Avoids a fork but is hacky.
3. **Temporary fork.** Add `Notify` in a fork and pin to it until upstream merges.

Note: the **TypeScript/Bun bridge alternative discussed below sidesteps this gap entirely** — the TS SDK supports arbitrary notifications natively. The Go-vs-TS choice is therefore upstream of this list (see Trade-offs and Open Questions).

### What the original draft proposed and why we're dropping it

The earlier draft of this proposal proposed:

- A new `task_events` table for an incremental, channel-shaped event log.
- A new `PollEventsRequest` / `PollEventsResponse` Connect RPC the agent process would poll every few seconds with a cursor.
- A `channel/channel` capability bolted onto the in-container `xagent mcp` process so it could push events into the *in-container* Claude Code agent.

All three are superseded:

- The C2 server **already publishes a `model.Notification` on every relevant mutation** and **already fans them out over SSE** at `/events`. A new table and a new polling RPC would duplicate that pipeline.
- The use case has moved to the local-developer Claude that drives the user-facing MCP server, not to in-container agents. The original framing of "replace the in-container agent's `get_my_task` polling with channels" is preserved as future work but is no longer the primary motivation.

The "Capability Declaration", "Server.Run Refactoring", and "Runner Integration" sections from the prior draft are replaced by the bridge command and `AddTools` refactor described above; "Database Changes" and "Event Polling" are removed entirely.

## Trade-offs

### Go bridge vs. TypeScript/Bun bridge

The prior draft rejected a TypeScript channel server because it would have dragged a Node runtime *into container images*. That objection no longer applies: the bridge runs on the developer's local machine, and **Bun is already a prerequisite for Claude Code Channels** — every official channel plugin in the preview ships as a Bun script. So the runtime cost of TS is essentially zero on a machine that already has channels working.

- **Go bridge** (`xagent mcp` subcommand): reuses `xagentclient`, `internal/x/sse`, `internal/server/mcpserver` tool definitions, and `model.Notification` directly. No code duplication; lives in the existing repo, ships with the existing release pipeline (single static binary). Cost: the Go MCP SDK has no public API for arbitrary notifications, so option 1, 2, or 3 above must be picked.
- **TS/Bun bridge**: the official `@modelcontextprotocol/sdk` server has first-class `notification()` support, so the channel-side problem is trivial. Cost: re-implements the user-facing tool proxying, Connect-client transport, auth token handling, and SSE parsing in TypeScript, and adds a second release artifact in a new language for the project to maintain.

This is the central open question (see below).

### Push into the bridge vs. point Claude at the existing HTTP endpoint

We could leave `/mcp` as the only entry point and ship a separate, minimal stdio "channel-only" subprocess whose sole job is to translate SSE → notifications. Pros: smaller surface; Claude can keep talking to the proven HTTP endpoint for tools. Cons: developers configure two MCP entries; the channel server still needs auth, an SSE client, and Bun-or-Go runtime — most of the bridge's complexity — without the simplicity win of "one stdio entry replaces everything for local use." We propose the bundled bridge as the default, but the split layout remains a valid alternative.

### Reusing the existing SSE stream vs. building a per-task subscription

The notify SSE stream is per-org: a bridge subscribes once and sees every notification the user's org generates. We could instead build a per-task channel-shaped endpoint and have the bridge open one subscription per task it has touched. Cons: more state on the bridge, more reconnect/lifecycle handling, more endpoints on the server. Pro of going per-task: trivially scoped — no risk of leaking another user's activity into the local Claude. We propose reusing the existing org-scoped stream and filtering in the bridge, but the filtering policy is itself an open question (next section).

## Open Questions

1. **Go or TypeScript/Bun for the bridge?** The container-runtime objection that drove the original Go preference is gone. Decide deliberately whether to absorb the upstream-Notify work (Go) or maintain a second small TypeScript artifact (TS). Both are real, neither is obviously right.
2. **Scope of forwarded notifications.** Should the bridge push every task notification on the org's SSE stream, or filter to tasks created by the same user (`Notification.UserID`) or even the same session (`Notification.ClientID`)? The `model.Notification` envelope carries both, so a filter is cheap; the policy choice (and the UX of "I created task 42 from this terminal — only tell me about it" vs. "tell me about everything in my org") is the question.
3. **Bridge-as-everything vs. channel-only bridge.** Should `xagent mcp` re-expose the user-facing tools alongside the channel (one MCP entry for the local user, as proposed), or stay channel-only and let the user keep pointing Claude at the HTTP `/mcp` endpoint for tools (two entries, sharper layering)?
4. **How rich should `content` be?** Channel `content` is the `<channel>` tag body. We could send a minimal `"Task 42 updated."` and rely on Claude calling `get_task`, or we could embed a short human-readable summary (status transition, instruction author) to save a round-trip. Richer `content` means the bridge fetches details before emitting, which costs an RPC per change.
5. **Permission relay.** Two-way channels and the `claude/channel/permission` capability would let xagent prompt the local Claude for approvals (e.g. before running a destructive task action). Out of scope for v1; flagged so we don't paint ourselves into a corner on transport/auth choices.

## Future work: pushing into in-container agents

The original framing — replacing the in-container `xagent mcp` process's reliance on the agent polling `get_my_task` with pushed channel events — is still achievable on top of this work once the Go SDK gap (resolution 1, 2, or 3 above) is closed. The in-container agent server already runs over stdio, so adding the capability is mechanically straightforward; the design question is which agent-side state changes (child completions, parent instructions, routed external events, child logs) deserve a push. That work is deferred to a follow-up proposal so this one can ship the local-developer use case first.
