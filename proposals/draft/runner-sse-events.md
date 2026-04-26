# Runner SSE Subscription Instead of Polling

Issue: https://github.com/icholy/xagent/issues/527

## Problem

The runner uses a fixed-interval polling loop (default 5 seconds) to discover pending task commands. Every cycle it calls `ListRunnerTasks`, which queries the database for tasks with `command != 0`. This introduces up to 5 seconds of latency before a new or restarted task is picked up, and generates unnecessary database load when nothing has changed.

The server already has a pub/sub system (`internal/pubsub/`) and an SSE endpoint (`/events`) that pushes real-time change notifications to the web UI. The runner should subscribe to this same notification stream so it reacts to task changes immediately.

## Design

### High-level approach

Replace the `time.Sleep`-based polling loop with an SSE subscription that triggers a poll whenever a relevant task change is published. The runner still calls `ListRunnerTasks` to get the authoritative list of pending commands — SSE only acts as a **wake-up signal**, not a replacement for the query. This keeps the design simple and avoids duplicating command-dispatch logic on the client side.

```
Before:  loop { Poll(); sleep(5s) }
After:   loop { Poll(); waitForSSEOrTimeout(30s) }
```

The runner calls `Poll()` immediately when it receives an SSE notification about a task change, or falls back to polling after a timeout (30 seconds) as a safety net for missed notifications.

### New SSE endpoint for runners

Add a runner-specific SSE endpoint that filters notifications to only task-related changes for the runner's org. This avoids sending irrelevant notifications (key changes, org member changes, etc.) to the runner.

**URL**: `GET /events/runner`

This reuses the existing `notifyserver` package and `pubsub.Subscriber` interface. The handler is nearly identical to the existing `handleSSE` but:
- Authenticates via API key (same as `ListRunnerTasks`).
- Does not filter out notifications from the same user (unlike the web UI endpoint which skips self-notifications).
- Only forwards notifications where at least one resource has `Type == "task"`.

```go
func (s *Server) handleRunnerSSE(w http.ResponseWriter, r *http.Request) {
    caller := apiauth.MustCaller(r.Context())
    orgID := caller.OrgID

    ch, cancel, err := s.subscriber.Subscribe(r.Context(), orgID)
    if err != nil {
        http.Error(w, "subscribe failed", http.StatusInternalServerError)
        return
    }
    defer cancel()

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming not supported", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    sw := sse.NewWriter(w)
    var seq int64

    // Send ready event
    data, _ := json.Marshal(model.Notification{Type: "ready", OrgID: orgID})
    sw.Write(sse.Event{ID: "0", Event: "ready", Data: data})
    flusher.Flush()

    ctx := r.Context()
    for {
        select {
        case n := <-ch:
            // Only forward task-related notifications
            hasTask := false
            for _, r := range n.Resources {
                if r.Type == "task" {
                    hasTask = true
                    break
                }
            }
            if !hasTask {
                continue
            }
            seq++
            data, err := json.Marshal(n)
            if err != nil {
                continue
            }
            if err := sw.Write(sse.Event{
                ID:    strconv.FormatInt(seq, 10),
                Event: n.Type,
                Data:  data,
            }); err != nil {
                return
            }
            flusher.Flush()
        case <-ctx.Done():
            return
        }
    }
}
```

### Route registration

In `internal/command/server.go`, register the new endpoint alongside the existing `/events`:

```go
mux.Handle("/events/runner", alice.New(s.auth.CheckAuth(), s.auth.AttachUserInfo()).ThenFunc(notifySrv.RunnerHandler()))
```

### Runner-side SSE client

Add a new `SSESubscriber` to the runner package that connects to the server's SSE endpoint and exposes a channel that fires when a task-related notification arrives.

```go
// SSESubscriber connects to the server's SSE endpoint and signals
// when task changes occur. It handles reconnection automatically.
type SSESubscriber struct {
    baseURL    string
    token      string
    log        *slog.Logger
    notify     chan struct{}
}

func NewSSESubscriber(baseURL, token string, log *slog.Logger) *SSESubscriber {
    return &SSESubscriber{
        baseURL: baseURL,
        token:   token,
        log:     log,
        notify:  make(chan struct{}, 1),
    }
}

// C returns a channel that receives a value when task changes occur.
func (s *SSESubscriber) C() <-chan struct{} {
    return s.notify
}

// Run connects to the SSE endpoint and processes events.
// It reconnects automatically on disconnection with backoff.
func (s *SSESubscriber) Run(ctx context.Context) error {
    for {
        err := s.connect(ctx)
        if ctx.Err() != nil {
            return ctx.Err()
        }
        s.log.Warn("SSE connection lost, reconnecting", "error", err)
        // Signal a poll on disconnect to catch any missed changes
        s.signal()
        if !common.SleepContext(ctx, 5*time.Second) {
            return ctx.Err()
        }
    }
}

func (s *SSESubscriber) connect(ctx context.Context) error {
    url := strings.TrimRight(s.baseURL, "/") + "/events/runner"
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return err
    }
    req.Header.Set("Authorization", "Bearer " + s.token)
    req.Header.Set("X-Auth-Type", "key")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("unexpected status: %d", resp.StatusCode)
    }

    scanner := bufio.NewScanner(resp.Body)
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "event: change") {
            s.signal()
        }
    }
    return scanner.Err()
}

func (s *SSESubscriber) signal() {
    select {
    case s.notify <- struct{}{}:
    default:
        // Already signaled, no need to queue another
    }
}
```

The SSE client uses a simple `bufio.Scanner` to read the event stream. It doesn't need to parse the full SSE protocol — it only cares about detecting `event: change` lines to trigger a poll. The channel is buffered with size 1 and uses non-blocking sends to coalesce multiple rapid notifications into a single poll.

### Updated runner loop

In `internal/command/runner.go`, replace the fixed-interval sleep with a select on the SSE channel:

```go
// Start SSE subscriber in background
var sseCh <-chan struct{}
if !strings.HasPrefix(serverAddr, "unix://") {
    sub := runner.NewSSESubscriber(serverAddr, cfg.Token, log)
    sseCh = sub.C()
    go func() {
        if err := sub.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
            log.Error("SSE subscriber error", "error", err)
        }
    }()
}

// Main loop
for {
    if err := r.Poll(ctx); err != nil {
        log.Error("failed to poll tasks", "error", err)
    }

    // Wait for SSE notification or fall back to polling after timeout
    select {
    case <-sseCh:
        // Task change detected, poll immediately
    case <-time.After(30 * time.Second):
        // Safety-net poll in case SSE missed something
    case <-ctx.Done():
        return nil
    }
}
```

Key behaviors:
- **Immediate reaction**: When a task is created, updated, restarted, or cancelled, the server publishes a notification. The runner's SSE subscription receives it and triggers an immediate poll.
- **Fallback polling**: If SSE is disconnected or a notification is missed, the runner still polls every 30 seconds as a safety net. This is 6x less frequent than today's 5-second interval.
- **Graceful degradation**: If the SSE connection fails entirely (e.g., server doesn't support the endpoint), the runner falls back to pure polling at the 30-second interval. When using a Unix socket connection, SSE is skipped entirely and the existing polling behavior is preserved.
- **Coalescing**: Multiple rapid notifications (e.g., bulk task creation) coalesce into a single poll via the buffered channel.

### Keeping the `--poll` flag

The `--poll` flag is repurposed as the fallback timeout instead of the primary polling interval:

```go
&cli.DurationFlag{
    Name:  "poll",
    Usage: "Fallback poll interval when SSE is unavailable",
    Value: 30 * time.Second,
},
```

### What does NOT change

- **`ListRunnerTasks` RPC**: Still the authoritative source for pending commands. SSE is only a wake-up signal.
- **`SubmitRunnerEvents` RPC**: Runner events are still submitted the same way.
- **`EventQueue`**: The event queue and its drain goroutine are unchanged.
- **`Monitor`**: Docker event monitoring is unchanged.
- **`Reconcile`**: Startup reconciliation is unchanged.
- **`Prune`**: Container pruning continues on its own interval.
- **Pub/sub layer**: `LocalPubSub` and the `Publisher`/`Subscriber` interfaces are unchanged.
- **Web UI SSE**: The existing `/events` endpoint for the web UI is unaffected.
- **Notification model**: `model.Notification` and `model.NotificationResource` are unchanged.

## Trade-offs

**SSE wake-up vs. server-push with full task data**: The chosen approach uses SSE purely as a trigger to call `ListRunnerTasks`. An alternative would be to push the full task command payload via SSE, eliminating the follow-up RPC. This was rejected because:
- It would duplicate the command-dispatch logic between the SSE publisher and the existing `ListRunnerTasks` handler.
- The notification system was designed for lightweight change signals, not data transfer.
- The follow-up `ListRunnerTasks` call is cheap (single indexed query) and ensures the runner always has the latest state.

**Server-streaming RPC vs. HTTP SSE**: Connect RPC supports server-streaming RPCs (`rpc WatchRunnerTasks(...) returns (stream ...)`) which would be more type-safe and avoid HTTP-level SSE parsing. This was rejected because:
- The existing SSE infrastructure (`notifyserver`, `pubsub`, `sse.Writer`) is already proven and deployed.
- A streaming RPC would require a separate pub/sub subscription path, duplicating plumbing.
- The runner's SSE client is trivial (~50 lines) since it only needs to detect change events, not parse structured data.

**Dedicated `/events/runner` endpoint vs. reusing `/events`**: The existing `/events` endpoint filters out self-notifications (`n.UserID == caller.ID`) which would cause the runner to miss notifications about its own task updates (e.g., when `SubmitRunnerEvents` triggers a publish). A dedicated endpoint avoids this issue and also filters out non-task notifications (key changes, org member changes) that are irrelevant to the runner.

**30-second fallback vs. no fallback**: The fallback poll ensures correctness even if SSE has gaps. Without it, a missed notification could leave a task stuck until the next change event. 30 seconds is infrequent enough to be low-overhead but short enough to bound worst-case latency.

## Open Questions

1. **Debounce window**: Should there be a short debounce (e.g., 100ms) after receiving an SSE notification before polling? This would coalesce rapid-fire notifications (e.g., creating 10 tasks at once) into a single poll. The current design relies on the buffered channel for coalescing, which works but means the runner might poll twice in quick succession if a second notification arrives between the first signal and the poll completing.

2. **Runner-scoped notifications**: The current pub/sub fans out by org ID. If multiple runners share an org, each runner receives notifications for all runners' tasks. This is fine for correctness (the `ListRunnerTasks` query filters by runner ID) but could be noisy. Should `Notification` gain a `Runner` field so the SSE endpoint can filter server-side? This is an optimization — probably not worth it unless there are many runners per org.

3. **Unix socket support**: The SSE client uses HTTP over TCP. Runners that connect to the server via Unix socket (`unix:///path`) would need either a custom HTTP transport for SSE or should fall back to pure polling. The current design skips SSE for Unix socket connections. Is this acceptable?
