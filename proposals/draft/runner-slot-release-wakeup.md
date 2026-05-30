# Runner Wake-Up When a Concurrency Slot Frees

Issue: https://github.com/icholy/xagent/issues/694

## Problem

The runner's main loop is now SSE-driven (see [runner-sse-events](runner-sse-events.md)):

```go
for {
    r.Poll(ctx)
    select {
    case <-sub.C():               // server notification: task command changed
    case <-time.After(pollInterval): // fallback, default 30s
    case <-ctx.Done():
        return nil
    }
}
```

`r.Poll` walks the tasks returned by `ListRunnerTasks` and, for every `Start`/`Restart` command, calls `r.sem.TryAcquire(1)`. When the concurrency limit is reached the task is silently skipped and left in the DB with its `Command` still set:

```go
// internal/runner/runner.go:206-210
if !r.sem.TryAcquire(1) {
    r.log.Debug("concurrency limit reached, skipping task", "task", task.ID, "limit", r.concurrency)
    return nil
}
```

The semaphore is released when `Monitor` sees a container `die` event (`runner.go:539`). That release is invisible to the server — it does not change any task command, so no `change` notification is published and `sub.C()` never fires. The skipped task therefore has to wait for the next fallback poll, which defaults to **30 seconds** and is the same interval used everywhere SSE could miss something.

Concretely, with `--concurrency 2` and three pending `Start` commands:

1. `Poll` starts tasks A and B, skips C (sem full).
2. A exits → `Monitor` calls `r.sem.Release(1)` and enqueues a `stopped` event.
3. The server applies `stopped`, transitioning A to `Done` with no command — `Notification.Runner` is empty (per the invariant from [runner-sse-events](runner-sse-events.md)), so the runner's SSE filter drops the notification.
4. C sits idle until the 30s fallback expires.

The pre-SSE polling loop had the same problem in principle, but its 5s interval masked it. With SSE-only signalling for new work and a 30s safety-net interval, the latency is now user-visible.

## Design

### Add a local wake-up channel on `Runner`

Treat a freed semaphore slot as a third wake-up source for the main loop, peer to SSE and the fallback timer. The runner already owns the semaphore, so the signal stays inside the same package and does not require any server- or proto-level changes.

```go
// internal/runner/runner.go
type Runner struct {
    // ... existing fields ...
    wake chan struct{}
}

func New(opts Options) (*Runner, error) {
    // ...
    return &Runner{
        // ...
        wake: make(chan struct{}, 1),
    }, nil
}

// WakeC returns a channel that receives one value per coalesced burst of
// internally-generated wake-ups (currently: semaphore slot releases).
func (r *Runner) WakeC() <-chan struct{} { return r.wake }

func (r *Runner) signalWake() {
    select {
    case r.wake <- struct{}{}:
    default:
    }
}
```

A size-1 buffered channel with non-blocking send is the same pattern `SSESubscriber.signal` uses (`sse.go:127`), so bursts coalesce into a single `Poll`.

### Signal on slot release

There is exactly one slot-release site that is *not* already inside `Poll`: the `die` event handler in `Monitor`. The two release sites at `runner.go:174` and `runner.go:212` are inside the same `Poll` invocation that will iterate to any remaining tasks itself, so they do not need a wake-up.

```go
// internal/runner/runner.go, inside Monitor's die case
case events.ActionDie:
    r.sem.Release(1)
    r.signalWake()
    // ... existing event-enqueue logic ...
```

### Select on the wake channel in the main loop

```go
// internal/command/runner.go
for {
    if err := r.Poll(ctx); err != nil {
        log.Error("failed to poll tasks", "error", err)
    }
    select {
    case <-sub.C():
    case <-r.WakeC():
    case <-time.After(pollInterval):
    case <-ctx.Done():
        return nil
    }
}
```

That is the entire behavioural change. Latency from "slot freed" to "next start attempt" drops from up to `pollInterval` (default 30s) to roughly the time it takes the docker events stream to deliver the `die` event plus one `ListRunnerTasks` round-trip.

### Test

Add a unit test in `internal/runner/runner_test.go` that:

1. Constructs a `Runner` with `Concurrency: 1`.
2. Calls `r.sem.TryAcquire(1)` so the semaphore is full.
3. Calls a small exported helper that runs the same body as `Monitor`'s `die` handler against a synthesised event (or refactors that body into a `handleDie` method that the test can call directly).
4. Asserts that `<-r.WakeC()` is non-blocking and that the semaphore count returned to zero.

This avoids needing a live docker daemon in the test (the existing `TestRunnerStart` already pays that cost; the wake-up logic does not need to).

## Trade-offs

**Local wake channel vs. piggy-backing on the SSE notification stream.** An alternative would be to make the server publish a notification whenever a `stopped`/`failed` event lands, *regardless* of whether the task ends up with a pending command, and let the runner's existing SSE path wake up the loop. That re-introduces the firehose problem [runner-sse-events](runner-sse-events.md) deliberately solved with `PendingRunner` and forces every runner in the org to wake up on every other runner's container exits. Keeping the signal local to the affected runner is strictly more precise and avoids touching the notification contract.

**Wake unconditionally vs. only when the sem was previously saturated.** We could skip the wake if `r.sem.Count() < r.concurrency` *before* the release, since no task was being blocked. The saving is one `ListRunnerTasks` call per non-saturated container exit — negligible against the cost of a docker container lifecycle. Unconditional signalling keeps the code branch-free and the invariant ("a die always wakes the loop") easy to reason about. (`Semaphore.Count()` does not currently exist anyway.)

**Wake channel vs. calling `Poll` directly from `Monitor`.** Calling `Poll` from inside the docker-events goroutine would couple Monitor to the dispatch logic, double the number of goroutines that can be inside `Poll` concurrently, and complicate shutdown. Funnelling through the existing main-loop select is cheaper.

**Shrinking the fallback `pollInterval`.** Dropping the fallback from 30s to e.g. 5s would also mask the problem, at the cost of restoring the per-runner DB load SSE was introduced to eliminate. The fallback exists for "SSE is broken" scenarios; concurrency-limited skips are routine and deserve a precise signal, not a tighter safety net.

## Open Questions

1. **Should `Reconcile`'s reconstructed `stopped`/`failed` events also signal the wake channel?** On startup `Reconcile` enqueues events for containers that exited while the runner was down (`runner.go:289-302`) but does not touch the semaphore in that path — the `Set(runningCount)` call right above only counts *running* containers. The very next thing the main loop does is `Poll`, so an extra wake would be redundant. Leaving `Reconcile` alone seems right but worth confirming.

2. **Naming.** `WakeC` mirrors `SSESubscriber.C()`. An alternative is to fold both into a single `wakeups` channel passed into `Runner` and have `Monitor` and the SSE subscriber both signal it. That removes one `case` from the main-loop select but spreads the wake-up plumbing across two constructors. Current proposal keeps them separate; happy to flip if there is a preference for consolidating.
