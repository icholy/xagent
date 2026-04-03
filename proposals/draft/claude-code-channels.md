# Claude Code Channels support for xagent MCP server

Issue: https://github.com/icholy/xagent/issues/466

## Problem

xagent agents running inside containers interact with the orchestrator through a request-response MCP tool model. The agent must explicitly call `get_my_task` to discover new events, status changes, or instructions. There is no mechanism for xagent to proactively push information into a running Claude Code session.

Claude Code Channels (research preview, v2.1.80+) allow an MCP server to push events into a session via `notifications/claude/channel`. This would let xagent notify agents about child task completions, new parent instructions, external webhook events, and child task log output — without polling.

## Background: How Channels Work

A channel is an MCP server that declares `claude/channel` in its experimental capabilities and emits `notifications/claude/channel` notifications. Key contract:

- **Capability declaration**: `capabilities.experimental["claude/channel"] = {}` in the MCP server constructor
- **Notification format**: `method: "notifications/claude/channel"`, `params: { content: string, meta: Record<string, string> }` — content becomes the body of a `<channel>` tag, meta entries become tag attributes
- **Reply tools**: Optional. Standard MCP tools exposed alongside the channel for two-way communication
- **Permission relay**: Optional. `claude/channel/permission` capability allows forwarding tool approval prompts remotely
- **Transport**: stdio only (Claude Code spawns the server as a subprocess)
- **Instructions**: A string added to Claude's system prompt describing what events to expect

Events arrive in Claude's context as:
```
<channel source="xagent" task_id="42" event_type="child_completed">
Child task 43 (fix auth bug) completed successfully.
</channel>
```

## Design

### Architecture

The xagent MCP server (`xagent mcp`) already runs as a stdio subprocess inside each agent container. The channel capability would be added to this same process rather than introducing a separate channel server.

```
Claude Code agent
    ↕ stdio (MCP tools + channel notifications)
xagent mcp process
    ↕ HTTP over unix socket
xagent server
```

The `xagent mcp` process gains a background goroutine that polls the xagent server for new events and pushes them as channel notifications to Claude Code.

### Go MCP SDK Gap

The Go MCP SDK v1.2.0 (`github.com/modelcontextprotocol/go-sdk`) supports setting experimental capabilities via `ServerOptions.Capabilities.Experimental`, but **does not expose a public API to send arbitrary notifications**. The internal `handleNotify` function and `ServerSession.getConn()` are both unexported.

The existing public notification methods (`NotifyProgress`, `Log`, `ResourceUpdated`) only send predefined MCP notification types — none support the custom `notifications/claude/channel` method.

**Resolution options (in order of preference):**

1. **Contribute upstream**: Add a `ServerSession.Notify(ctx, method string, params any) error` method to the Go SDK. This is a small, well-scoped change — it just needs to call `conn.Notify()` on the underlying jsonrpc2 connection. This would unblock all Go-based channel implementations.

2. **Use the jsonrpc2 layer directly**: The `internal/jsonrpc2.Connection` type has a public `Notify(ctx, method, params)` method. We could create a custom `mcp.Transport` wrapper that intercepts the connection before it's passed to the SDK, retaining a reference for direct notification sending. This is hacky but avoids forking.

3. **Separate TypeScript channel process**: Run a small Node/Bun process as the channel server alongside `xagent mcp`. The TypeScript MCP SDK has native channel support. The channel process would communicate with the Go `xagent mcp` process (or directly with the xagent server) to get events. This adds operational complexity (Node runtime in containers).

4. **Fork the Go SDK temporarily**: Add the `Notify` method in a fork, use it until upstream merges the change.

Option 1 is strongly preferred. The change is minimal and benefits the broader Go MCP ecosystem.

### Event Polling

The `xagent mcp` process would start a background goroutine after connecting:

```go
// After server.Run starts (requires refactoring to use server.Connect directly)
go s.pollEvents(ctx, session)
```

The poll loop would call a new RPC endpoint on the xagent server:

```protobuf
message PollEventsRequest {
  int64 task_id = 1;
  int64 after_event_id = 2;  // cursor for incremental polling
}

message PollEventsResponse {
  repeated TaskEvent events = 1;
}

message TaskEvent {
  string type = 1;           // "child_completed", "child_failed", "instruction_added", "external_event", "child_log"
  string content = 2;        // human-readable description
  map<string, string> meta = 3;  // routing attributes
  int64 id = 4;              // monotonic ID for cursor
}
```

This is a new endpoint because the existing `GetTaskDetails` returns the full task state — we need an incremental, cursor-based stream of changes.

### Event Types

| Type | Trigger | Content | Meta |
|------|---------|---------|------|
| `child_completed` | Child task status → COMPLETED | "Child task {id} ({name}) completed" | `task_id`, `child_id` |
| `child_failed` | Child task status → FAILED | "Child task {id} ({name}) failed" | `task_id`, `child_id` |
| `instruction_added` | Parent adds instruction via `update_child_task` | The instruction text | `task_id`, `source` |
| `external_event` | Webhook routed via subscribed link | Event description + data | `task_id`, `event_id`, `url` |
| `child_log` | Child task uploads a log with type "llm" | The log message | `task_id`, `child_id` |

### Capability Declaration

In `internal/command/mcp.go`, change the server constructor:

```go
server := mcp.NewServer(&mcp.Implementation{
    Name:    "xagent",
    Version: "1.0.0",
}, &mcp.ServerOptions{
    Capabilities: &mcp.ServerCapabilities{
        Experimental: map[string]any{
            "claude/channel": map[string]any{},
        },
    },
    Instructions: "Events from the xagent channel arrive as <channel source=\"xagent\" ...>. " +
        "They notify you about task status changes, new instructions, and external events. " +
        "You do not need to reply to these events — they are informational. " +
        "Use the existing xagent MCP tools to take action based on them.",
})
```

### Server.Run Refactoring

Currently `xagent mcp` calls `server.Run(ctx, &mcp.StdioTransport{})` which blocks. To start the poll goroutine, we need the `ServerSession`:

```go
session, err := server.Connect(ctx, &mcp.StdioTransport{}, nil)
if err != nil {
    return err
}
go pollEvents(ctx, session, client, task)
return session.Wait()
```

### Runner Integration

The runner (`internal/runner/runner.go`) injects the `xagent` MCP server config into each container. For channels, Claude Code needs a separate `--channels` flag. The runner would need to:

1. Detect if the Claude Code version supports channels (v2.1.80+)
2. Pass `--dangerously-load-development-channels server:xagent` during research preview, or `--channels server:xagent` once allowlisted
3. Add the xagent MCP server to the channels config section

This requires changes to `internal/agent/config.go` and `internal/agent/claude.go` to support the channels CLI flag.

### Database Changes

Add an `events_log` table to store the incremental event stream:

```sql
CREATE TABLE task_events (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES tasks(id),
    type TEXT NOT NULL,
    content TEXT NOT NULL,
    meta JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_task_events_task_id_id ON task_events(task_id, id);
```

Events are written by the server when task state changes occur (status updates, new instructions, webhook events). The `xagent mcp` process polls `task_events WHERE task_id = ? AND id > ?`.

### Permission Relay (Future)

Permission relay (`claude/channel/permission`) is not in scope for the initial implementation. It would require a trusted sender path (e.g., the web UI or a chat integration) and adds significant complexity around authentication and UX. This can be added later once the one-way channel is proven.

## Trade-offs

### Why modify the existing `xagent mcp` process vs. a separate channel server?

A separate channel server (e.g., in TypeScript) would sidestep the Go SDK gap but adds:
- Node.js runtime dependency in Docker containers
- IPC between the channel process and the Go MCP process or xagent server
- Two separate MCP server configs to manage
- More failure modes

Modifying the existing process keeps everything in one binary, reuses the existing auth/transport, and is architecturally simpler. The Go SDK gap is the only blocker and is solvable.

### Why polling vs. server-push (WebSocket/SSE)?

The `xagent mcp` process communicates with the server over a unix socket HTTP transport. Adding WebSocket or SSE support to the unix socket proxy (`internal/runner/proxy.go`) and the Connect RPC API is significant work. Polling every 2-5 seconds with a cursor is simple, efficient (small payloads), and consistent with the runner's existing polling pattern for task assignment.

### Why a new `task_events` table vs. reusing existing events?

The existing `events` table stores webhook payloads routed via subscribed links. Channel events are broader — they include task status changes and log forwarding which aren't external webhooks. A dedicated table with a simple monotonic ID makes cursor-based polling trivial and avoids complicating the existing event routing system.

## Open Questions

1. **Go SDK upstream appetite**: Would the `modelcontextprotocol/go-sdk` maintainers accept a `ServerSession.Notify` method? This should be validated before starting implementation.

2. **Poll interval**: What's the right balance between responsiveness and load? 2 seconds seems reasonable but should be configurable. Long-polling would be better but requires more transport work.

3. **Event retention**: How long should `task_events` rows be kept? Options: delete when task is archived, TTL-based cleanup, or keep indefinitely. Archival cleanup is simplest and aligns with existing patterns.

4. **Research preview constraints**: Channels require `--dangerously-load-development-channels` for custom servers. Should xagent wait for a stable release, or ship with the development flag during the preview? The flag requires user confirmation on each launch.

5. **Which events to start with**: The full set (child status, instructions, external events, child logs) may be too ambitious for v1. Starting with just `external_event` (webhook forwarding) would deliver the highest value with the least new infrastructure, since events already exist in the database.

6. **Channel vs. tool hybrid**: Should channel events supplement or replace the polling done by `get_my_task`? If Claude receives a channel event about a child completing, it may still need to call `get_my_task` or `list_child_tasks` to get the full details. The channel serves as a notification layer, not a data layer.
