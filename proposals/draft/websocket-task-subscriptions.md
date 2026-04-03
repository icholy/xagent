# WebSocket endpoint for real-time task subscriptions with Redis pub/sub

Issue: https://github.com/icholy/xagent/issues/470

## Problem

The web UI polls every 6 seconds (`refetchInterval: 6000`) for task updates across all views (task list, task details, logs, events). This introduces visible latency, wastes bandwidth when nothing has changed, and scales poorly with more clients. There is no mechanism to push task state changes to connected browsers.

With multiple server instances behind a load balancer, one instance may process a task update while clients are connected to a different instance. A shared pub/sub bus is needed for cross-instance notification.

## Design

### WebSocket endpoint

**URL**: `GET /ws/tasks`

**Query parameters**:
- `org_id` (required) — scopes the subscription to a single org
- `task_id` (optional, repeatable) — subscribe to specific tasks only. If omitted, receive all task changes for the org.

**Authentication**: The endpoint reuses the existing `CheckAuth()` + `AttachUserInfo()` middleware chain from `server.go`. Cookie auth and Bearer tokens both work. The WebSocket upgrade happens after auth succeeds. Unauthenticated requests get a 401 before upgrade.

**Wire format** (JSON over WebSocket text frames):

```json
{
  "type": "task_updated",
  "task_id": 42,
  "org_id": 7,
  "version": 15,
  "timestamp": "2026-04-03T12:00:00Z"
}
```

Message types:
- `task_updated` — a task's state changed (status, name, instructions, command, version)
- `task_created` — a new task was created in the org
- `task_archived` — a task was archived/unarchived
- `log_appended` — new log entry for a task
- `link_created` — new link added to a task
- `event_added` — new event associated with a task

Messages are **lightweight notifications**, not full payloads. The client uses the notification as a signal to invalidate the relevant TanStack Query cache and refetch only the affected data via the existing Connect RPC API. This avoids duplicating serialization logic and keeps the WebSocket protocol simple.

**Ping/pong**: Server sends WebSocket pings every 30 seconds. Clients that don't respond within 10 seconds are disconnected.

### Redis pub/sub integration

**Channel naming**: `xagent:org:{org_id}:tasks`

One Redis channel per org. All task change notifications for an org are published to the same channel. This keeps the channel count bounded by the number of orgs (not tasks).

**Publish flow**:

1. Server RPC handler mutates a task (e.g., `UpdateTask`, `CancelTask`, `SubmitRunnerEvents`)
2. After the database write commits, the handler publishes a notification to Redis
3. The publish is fire-and-forget — if Redis is down, clients fall back to polling

**Implementation**: Add a `Publisher` interface to the server:

```go
// internal/pubsub/pubsub.go

type Notification struct {
    Type    string `json:"type"`
    TaskID  int64  `json:"task_id"`
    OrgID   int64  `json:"org_id"`
    Version int64  `json:"version"`
    Time    time.Time `json:"timestamp"`
}

type Publisher interface {
    Publish(ctx context.Context, orgID int64, n Notification) error
}

type Subscriber interface {
    Subscribe(ctx context.Context, orgID int64) (<-chan Notification, func(), error)
}
```

**Redis implementation** using `github.com/redis/go-redis/v9`:

```go
// internal/pubsub/redis.go

type RedisPubSub struct {
    client *redis.Client
}

func (r *RedisPubSub) Publish(ctx context.Context, orgID int64, n Notification) error {
    data, _ := json.Marshal(n)
    return r.client.Publish(ctx, channelKey(orgID), data).Err()
}

func (r *RedisPubSub) Subscribe(ctx context.Context, orgID int64) (<-chan Notification, func(), error) {
    sub := r.client.Subscribe(ctx, channelKey(orgID))
    ch := make(chan Notification, 64)
    go func() {
        defer close(ch)
        for msg := range sub.Channel() {
            var n Notification
            if json.Unmarshal([]byte(msg.Payload), &n) == nil {
                ch <- n
            }
        }
    }()
    return ch, func() { sub.Close() }, nil
}

func channelKey(orgID int64) string {
    return fmt.Sprintf("xagent:org:%d:tasks", orgID)
}
```

**No-op implementation** for single-instance deployments or when Redis is not configured:

```go
// internal/pubsub/local.go

type LocalPubSub struct {
    mu   sync.RWMutex
    subs map[int64][]chan Notification
}
```

An in-process implementation that short-circuits through a local channel map. This makes Redis optional — single-instance deployments work without it.

### Server-side WebSocket handler

The handler lives in a new file `internal/server/websocket.go`. It uses `nhooyr.io/websocket` (the standard Go WebSocket library).

```go
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    caller := apiauth.MustCaller(r.Context())

    conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
        // CORS handled by existing middleware
    })
    if err != nil {
        return
    }
    defer conn.CloseNow()

    orgID := caller.OrgID
    taskIDs := parseTaskIDs(r.URL.Query()["task_id"])

    ch, cancel, err := s.subscriber.Subscribe(r.Context(), orgID)
    if err != nil {
        conn.Close(websocket.StatusInternalError, "subscribe failed")
        return
    }
    defer cancel()

    for n := range ch {
        if len(taskIDs) > 0 && !taskIDs.Contains(n.TaskID) {
            continue
        }
        data, _ := json.Marshal(n)
        if err := conn.Write(r.Context(), websocket.MessageText, data); err != nil {
            return
        }
    }
}
```

**Registration in `Handler()`**:

```go
mux.Handle("/ws/tasks", alice.New(s.auth.CheckAuth(), s.auth.AttachUserInfo()).ThenFunc(s.handleWebSocket))
```

### Publish points in existing handlers

Each server RPC handler that mutates task state adds a publish call after the database commit. The `Server` struct gains a `Publisher` field:

```go
type Server struct {
    // ... existing fields
    publisher pubsub.Publisher
}
```

Handlers that publish:
- `CreateTask` → `task_created`
- `UpdateTask` → `task_updated`
- `ArchiveTask` / `UnarchiveTask` → `task_archived`
- `CancelTask` → `task_updated`
- `RestartTask` → `task_updated`
- `SubmitRunnerEvents` → `task_updated` (status/version changes from runner)
- `UploadLogs` → `log_appended`
- `CreateLink` → `link_created`
- `AddEventTask` → `event_added`

### Web UI integration

**New hook** in `webui/src/lib/useTaskWebSocket.ts`:

```typescript
import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

interface TaskNotification {
  type: string;
  task_id: number;
  org_id: number;
  version: number;
  timestamp: string;
}

export function useTaskWebSocket(taskIds?: number[]) {
  const queryClient = useQueryClient();

  useEffect(() => {
    const params = new URLSearchParams();
    taskIds?.forEach(id => params.append("task_id", String(id)));

    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${protocol}//${location.host}/ws/tasks?${params}`);

    ws.onmessage = (event) => {
      const notification: TaskNotification = JSON.parse(event.data);

      switch (notification.type) {
        case "task_created":
        case "task_updated":
        case "task_archived":
          queryClient.invalidateQueries({ queryKey: ["xagent.v1.XAgentService", "ListTasks"] });
          queryClient.invalidateQueries({ queryKey: ["xagent.v1.XAgentService", "GetTaskDetails"] });
          break;
        case "log_appended":
          queryClient.invalidateQueries({ queryKey: ["xagent.v1.XAgentService", "ListLogs"] });
          break;
        case "link_created":
        case "event_added":
          queryClient.invalidateQueries({ queryKey: ["xagent.v1.XAgentService", "GetTaskDetails"] });
          break;
      }
    };

    ws.onclose = () => {
      // Reconnect with backoff (see reconnection strategy below)
    };

    return () => ws.close();
  }, [taskIds, queryClient]);
}
```

**Usage in routes**: Replace `refetchInterval: 6000` with the WebSocket hook. Keep a slow poll (e.g., 60 seconds) as a fallback in case the WebSocket disconnects.

```typescript
// tasks.index.tsx
useTaskWebSocket();
const { data } = useQuery(listTasks, {}, { refetchInterval: 60000 });
```

### Reconnection strategy

The client implements exponential backoff on disconnect:
- Initial delay: 1 second
- Max delay: 30 seconds
- Backoff factor: 2x
- Jitter: random 0-1s added to each delay
- On successful reconnect, immediately invalidate all queries to catch up on missed updates

### Configuration

New server flags:

```
--redis-url         Redis connection URL (optional, enables Redis pub/sub)
--ws-ping-interval  WebSocket ping interval (default: 30s)
```

When `--redis-url` is not set, the server uses `LocalPubSub` for single-instance deployments.

### Scaling considerations

- **Multiple server instances**: Redis pub/sub ensures all instances receive notifications. Each instance maintains its own WebSocket connections and forwards relevant notifications.
- **Redis is not persistent**: If Redis restarts, subscribers reconnect automatically (go-redis handles this). Clients may miss notifications during the gap, but the fallback poll (60s) catches up.
- **Connection limits**: Each WebSocket is a long-lived TCP connection. The server should enforce a max connections per org (e.g., 100) to prevent resource exhaustion. Return HTTP 429 when the limit is reached.
- **Channel fan-out**: Publishing is O(1) per org regardless of subscriber count — Redis handles the fan-out. Server-side filtering by `task_id` happens after receiving from Redis, keeping the channel count low.

### Database changes

None. Redis is used as a transient message bus, not for persistent state. No schema migrations required.

### CLI changes

New flag on `xagent server`: `--redis-url` (env: `REDIS_URL`).

## Trade-offs

**WebSocket + invalidation vs. WebSocket + full payloads**: Sending lightweight notifications that trigger TanStack Query refetches is simpler than streaming full task objects over the WebSocket. It avoids duplicating proto-to-JSON serialization and leverages existing Connect RPC response caching. The tradeoff is an extra HTTP round-trip per notification, but since task updates are relatively infrequent (seconds, not milliseconds), this is acceptable.

**Redis pub/sub vs. PostgreSQL LISTEN/NOTIFY**: Postgres LISTEN/NOTIFY would avoid adding a new dependency, but it has a payload size limit (8KB), doesn't work across separate database connections cleanly, and adds load to the primary database. Redis pub/sub is purpose-built for this pattern and scales independently.

**Redis pub/sub vs. Redis Streams**: Streams provide persistence and consumer groups, but we don't need message durability — the WebSocket is a best-effort notification layer with polling as fallback. Pub/sub is simpler and lower latency.

**Server-Sent Events vs. WebSocket**: SSE is simpler (unidirectional, works over HTTP/2), but WebSocket allows future bidirectional communication (e.g., client sending subscription changes without reconnecting). WebSocket also has better library support in Go for ping/pong health checks.

**Per-org channels vs. per-task channels**: Per-task channels would reduce server-side filtering but could create millions of Redis subscriptions. Per-org channels are bounded and simple, with lightweight server-side filtering.

## Open Questions

1. **Should the WebSocket support dynamic subscription changes?** The current design requires reconnecting to change the `task_id` filter. An alternative is a client-to-server message to update subscriptions on the fly. This adds complexity but may be useful for the task detail view.

2. **Should we expose WebSocket to API consumers (not just the web UI)?** If so, we may need to document it as a public API and version it. For now, the proposal treats it as an internal web UI transport.

3. **Redis connection pooling and failure modes**: Should the server refuse to start if `--redis-url` is set but Redis is unreachable? Or should it fall back to `LocalPubSub` with a warning? The latter is more resilient but could mask configuration errors.

4. **Notification deduplication**: Multiple RPC calls may update the same task in quick succession. Should we debounce notifications (e.g., coalesce within 100ms)? The client-side TanStack Query already deduplicates refetches, so server-side debouncing may not be needed.
