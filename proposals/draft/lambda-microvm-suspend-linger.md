# lambda-microvm: linger before suspend to absorb fast follow-up requests

Issue: https://github.com/icholy/xagent/issues/1106

## Problem

In the `lambda-microvm` backend, the runner suspends a MicroVM **immediately**
when the driver exits. `Backend.Wait`
(`internal/runner/backend/lambdamicrovm/lambdamicrovm.go`) reads the
`/xagent/lifecycle` SSE stream and, on a clean `driver-exited` event, calls
`SuspendMicrovm` before returning the exit code:

```go
exited, code := b.readStream(ctx, id, endpoint, token)
if exited {
    if _, err := b.cloud.SuspendMicrovm(ctx, &awsmicrovm.SuspendMicrovmInput{MicrovmID: id}); err != nil {
        b.log.Warn("wait: suspend after driver exit", "microvm", id, "error", err)
    }
    return backend.ExitCode(code), nil
}
```

A fast follow-up run of the same task (a webhook landing seconds after the agent
finishes, a linked PR comment) then hits a `SUSPENDED` VM and pays the full
resume cost: `resume-microvm` (un-snapshot latency) plus a driver re-spawn.
Waiting a short, configurable grace period before suspending would let that
follow-up reuse the still-warm, still-`RUNNING` VM.

## Design

### Why "just delay inside `Wait`" does not work

The obvious implementation — sleep in `Wait` between `driver-exited` and
`SuspendMicrovm` — cannot actually deliver warm reuse, for two reasons rooted in
how the runner drives the backend:

1. **A blocking `Wait` holds a concurrency slot.** `supervise` releases the
   runner's semaphore slot only *after* `Wait` returns
   (`internal/runner/runner.go`: `code, err := r.backend.Wait(...)` then
   `r.sem.Release(1)`). If `Wait` lingers for three minutes, the slot is held for
   three minutes. A follow-up `Start` does `r.sem.TryAcquire(1)`; under a
   saturated concurrency limit it fails, the task is skipped and retried next
   poll — until the window elapses, the suspend fires, and the follow-up resumes
   **cold**. The linger would defeat itself precisely when the runner is busy.

2. **The follow-up re-enters through `Start` → `Launch(reuse)`, not through the
   lingering goroutine.** By the time the driver exits it has already reported
   its terminal outcome to the server, so the task settles and the follow-up is a
   brand-new `Start` command on a later poll. That `Start` calls `Probe` /
   `Running`; a lingering VM answers `GetMicrovm` = `RUNNING`, so the runner
   would treat it as "already running, let it finish" and never re-spawn the
   driver — even though the driver has exited. The RUNNING-adoption branch in
   `tryResume` only *adopts* a handle; it does not re-spawn the driver.

So the linger has to (a) let `Wait` return promptly and release the slot, (b)
keep the suspend pending on a detached timer the backend owns, (c) make `Probe`
report the lingering VM as *exited* so the runner re-enters through
`Launch(reuse)`, and (d) give `Launch(reuse)` a way to re-spawn the driver in the
warm VM without a suspend/resume round-trip.

### `WarmCache`: the detached-suspend registry

All of the linger state and its concurrency live in a dedicated `WarmCache`
struct rather than as loose fields and methods on `Backend`. `Backend` holds one
and delegates to it; the state machine (schedule → exactly one of {suspend,
claim, flush}) is then testable in isolation, without a `Backend` or the
`Cloud`/`Stager` fakes.

```go
// WarmCache tracks MicroVMs whose driver has exited but whose suspend is
// deferred for a linger window, so a fast follow-up run can reclaim the still-
// RUNNING VM instead of paying a cold resume. It is keyed by MicroVM id (1:1
// with a task) and is safe for concurrent use.
type WarmCache struct {
    suspend func(id string) // fires the actual SuspendMicrovm (injected by Backend)
    log     *slog.Logger

    mu      sync.Mutex
    ctx     context.Context    // cache-lifetime ctx (survives any single Wait)
    cancel  context.CancelFunc
    entries map[string]*warmEntry // microvmID → pending suspend
}

type warmEntry struct {
    timer *time.Timer
    done  bool // scheduled → exactly one terminal: {suspended, claimed, flushed}
}

// Schedule defers suspend(id) by delay. A follow-up Claim within the window
// cancels it; the window elapsing fires it. delay == 0 suspends inline and
// registers nothing.
func (w *WarmCache) Schedule(id string, delay time.Duration)

// Claim cancels a pending suspend and returns true if it won the race against
// the timer (the VM is still RUNNING and safe to reclaim). It returns false if
// the suspend already fired or none was pending — the caller falls back to the
// cold resume path.
func (w *WarmCache) Claim(id string) bool

// Pending reports whether id has a live deferred suspend (drives Probe).
func (w *WarmCache) Pending(id string) bool

// Flush cancels every pending timer and suspends each VM now. Called from
// Backend.Close so no warm VM keeps billing after the runner stops.
func (w *WarmCache) Flush()
```

`Backend` owns one `WarmCache`, wiring `suspend` to the actual control-plane
call so the cache itself stays free of AWS types:

```go
func New(opts Options) (*Backend, error) {
    b := &Backend{ /* ...existing fields... */ }
    b.warm = NewWarmCache(func(id string) {
        if _, err := b.cloud.SuspendMicrovm(b.warm.ctx, &awsmicrovm.SuspendMicrovmInput{MicrovmID: id}); err != nil {
            b.log.Warn("warm: suspend after linger", "microvm", id, "err", err)
        }
    }, opts.Log)
    return b, nil
}
```

**`Wait` change.** On a clean `driver-exited`, instead of suspending inline,
`Wait` hands the VM to the cache and returns immediately:

```go
if exited {
    b.warm.Schedule(id, suspendDelay) // suspends inline when suspendDelay == 0
    return backend.ExitCode(code), nil
}
```

Because `Schedule` registers the entry *before* `Wait` returns, it exists before
any follow-up `Start` can observe the VM, closing the ordering gap. The timer
runs on the cache's own lifetime context, not the supervise `ctx` (which dies as
soon as `Wait` returns).

**`Probe` change.** A VM the cache still holds reports `StateExited`
(husk-preserved) even though `GetMicrovm` says `RUNNING`, so the runner's
`Running()` guard does not short-circuit the follow-up and instead drives
`Start` → `Probe` `StateExited` → `Launch(reuse)`:

```go
func (b *Backend) Probe(ctx context.Context, h backend.Handle) (backend.State, error) {
    if b.warm.Pending(h.ID) {
        return backend.StateExited, nil // warm husk: drive reuse, not "already running"
    }
    // ...existing GetMicrovm switch...
}
```

**`Close` change.** `Close` calls `b.warm.Flush()` — cancelling every pending
timer and suspending now rather than leaving warm VMs billing compute after the
runner stops. This differs deliberately from the mid-task shutdown contract
(where `Wait` returns without suspending so a *still-running* driver's VM
survives for rehydration): here the driver has already exited, so `SUSPENDED` is
the correct resting state and next boot resumes on demand.

### A single re-spawn path: the shim does not distinguish warm from cold

The driver re-spawn must look **identical to the shim** whether the VM was cold
(un-snapshotted by `ResumeMicrovm`) or warm (never suspended). So the runner owns
the re-spawn trigger uniformly: a single `POST /xagent/start` on the control
surface is the *only* way the driver is re-started, and the shim cannot tell
which path led there. This retires the one place the shim currently distinguishes
them — the AWS `/resume` hook re-spawning on its own.

**Shim: one re-spawn entry point, resume hook demoted to a thaw seam.** Today
`resumeHook` re-spawns the driver (`resumeHook` → `spawn`). Move that trigger to
the control surface and make the resume hook a pure thaw seam — symmetric with
`suspendHook`, which is already a pure flush seam:

```go
const lambdamicrovmStartPath = "/xagent/start"

func (s *Server) ControlHandler() http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("GET "+lambdamicrovmLifecyclePath, s.lifecycleHandler)
    mux.HandleFunc("POST "+lambdamicrovmStopPath, s.stopHandler)
    mux.HandleFunc("POST "+lambdamicrovmStartPath, s.startHandler)
    return mux
}

// startHandler (re)spawns the driver from the retained bundle. It is the sole
// re-spawn trigger — used identically after a cold ResumeMicrovm and a warm
// reclaim — so the shim never distinguishes the two. Idempotent: a no-op if a
// driver is already running (a duplicate POST, or a restart that beat the
// suspend), a 409 before first provisioning.
func (s *Server) startHandler(w http.ResponseWriter, _ *http.Request) {
    s.mu.Lock()
    running := s.current != nil && !isDone(s.current)
    bundle, started := s.bundle, s.started
    s.mu.Unlock()
    if !started {
        http.Error(w, "not provisioned", http.StatusConflict)
        return
    }
    if running {
        w.WriteHeader(http.StatusOK) // already running: idempotent no-op
        return
    }
    if err := s.spawn(bundle); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    w.WriteHeader(http.StatusOK)
}

// resumeHook becomes a thaw seam: the VM un-snapshots, but the driver is
// re-spawned by the runner's /xagent/start, not here.
func (s *Server) resumeHook(context.Context, awsmicrovm.ResumeHookRequest) error {
    return nil
}
```

The shim still legitimately separates *first provisioning* (the `/run` hook —
fetch bundle, provision files once, first spawn) from *re-spawn* (`/xagent/start`
— retained bundle, no re-fetch). That is a provisioning distinction, not a
warm/cold one. `spawn` already clears the sticky `driver-exited`
(`s.lc.reset()`), so the fresh `Wait` the runner starts blocks for the *new*
run's exit rather than replaying the old one.

**Runner: `tryResume` re-spawns the same way on every path.** After optionally
un-snapshotting, `tryResume` POSTs `/xagent/start` in all cases — warm claim,
cold resume, or plain adoption. Idempotency makes this safe uniformly (adopting a
still-running driver → the POST is a no-op):

```go
func (b *Backend) tryResume(ctx context.Context, reuse *backend.Handle) (bool, backend.Handle, error) {
    hd, _ := decodeData(reuse.Data)

    if b.warm.Claim(reuse.ID) {
        // Warm: won the race against the suspend timer; VM never left RUNNING,
        // so skip ResumeMicrovm entirely.
    } else {
        out, err := b.cloud.GetMicrovm(ctx, &awsmicrovm.GetMicrovmInput{MicrovmID: reuse.ID})
        if awsmicrovm.IsNotFound(err) {
            return false, backend.Handle{}, nil
        }
        if err != nil {
            return false, backend.Handle{}, fmt.Errorf("get microvm for resume: %w", err)
        }
        switch out.Microvm.State {
        case awsmicrovm.MicrovmStateRunning, awsmicrovm.MicrovmStatePending:
            // Adopt; /xagent/start below is a no-op if the driver still runs.
        case awsmicrovm.MicrovmStateSuspended:
            if _, err := b.cloud.ResumeMicrovm(ctx, &awsmicrovm.ResumeMicrovmInput{MicrovmID: reuse.ID}); err != nil {
                return false, backend.Handle{}, fmt.Errorf("resume microvm: %w", err)
            }
        default: // SUSPENDING / TERMINATING / TERMINATED
            return false, backend.Handle{}, nil
        }
        if out.Microvm.Endpoint != "" {
            hd.Endpoint = out.Microvm.Endpoint
        }
    }

    // One uniform re-spawn trigger, warm or cold. Retried over the same managed
    // proxy the SSE stream uses; /xagent/start is idempotent.
    if err := b.respawn(ctx, reuse.ID, hd.Endpoint); err != nil {
        return false, backend.Handle{}, fmt.Errorf("respawn driver: %w", err)
    }
    data, _ := json.Marshal(hd)
    return true, backend.Handle{Type: HandleType, ID: reuse.ID, Data: data}, nil
}
```

`Claim` transitions the cache entry to its terminal state under the cache mutex
and returns `true` only if it beat the suspend timer; if the suspend already
fired (or nothing was pending) it returns `false` and the cold branch runs — the
window simply elapsed. Exactly one of {suspend fires, claim wins} completes; the
loser is a no-op. `Destroy` likewise `Claim`s (discarding the result) so an
archive during the window cancels the pending timer before terminating.

`respawn` mints a proxy token and POSTs `/xagent/start`, retried with backoff
over the same managed-proxy reachability the SSE stream already depends on (the
runner must reach the shim to observe the driver at all). Because `/xagent/start`
is idempotent, a retry — or a POST to a VM whose driver is already running — is a
safe no-op.

### Configuration

Per-workspace, disabled by default:

```go
// internal/runner/workspace/workspace.go, LambdaMicroVM
// SuspendDelaySeconds delays suspending the MicroVM after the driver exits, so a
// fast follow-up run reuses the still-RUNNING VM instead of paying a cold
// resume. 0 (default) suspends immediately. Bounded well under max_duration.
SuspendDelaySeconds int64 `yaml:"suspend_delay_seconds"`
```

`ValidateWorkspace` rejects negative values and values `>= MaxDurationSeconds`
(the AWS reap floor — a linger must never outlive the VM). `Wait` needs the value
but only receives a `backend.Handle`, so the delay is **persisted into
`handleData`** at launch, where `launchFresh` / `tryResume` already read
`spec.Workspace.LambdaMicroVM`:

```go
type handleData struct {
    Endpoint            string `json:"endpoint"`
    ImageARN            string `json:"image_arn"`
    StageBucket         string `json:"stage_bucket"`
    StageKey            string `json:"stage_key"`
    SuspendDelaySeconds int64  `json:"suspend_delay_seconds,omitempty"`
}
```

`Wait` decodes it alongside `Endpoint`. Persisting in the handle keeps `Wait`'s
signature unchanged; the trade-off is that an operator edit to
`workspaces.yaml` takes effect on the next fresh launch, not retroactively for
in-flight handles.

## Implementation Plan

1. **Config + handle plumbing** — Add `SuspendDelaySeconds` to
   `workspace.LambdaMicroVM` with `ValidateWorkspace` bounds, add it to
   `handleData`, and populate it in `launchFresh` and `tryResume`. `Wait` decodes
   it but still suspends immediately (behavior unchanged). Delivers: the config
   surface and persisted delay. Depends on: nothing. Verifiable by: unit tests
   that the field round-trips through `handleData` and that validation rejects
   negative / `>= max_duration` values.

2. **Unify the shim re-spawn trigger** — Add `startHandler` (`POST
   /xagent/start`) as the sole driver re-spawn entry point, idempotent with an
   already-running guard, and demote `resumeHook` to a thaw-seam no-op. `tryResume`
   POSTs `/xagent/start` after every resume/adopt (via a `respawn` helper retried
   over the proxy) so the cold-resume path re-spawns the same way the warm path
   will. Delivers: one uniform re-spawn path the shim can't distinguish; the
   existing cold resume keeps working through it. Depends on: nothing new
   (reworks the existing resume path). Verifiable by: shim unit tests — after a
   driver exit `POST /xagent/start` re-spawns and publishes a fresh
   `driver-exited`, a second POST while running is a no-op, a POST before
   provisioning is a 409, and the resume hook alone no longer spawns; backend test
   that a cold resume POSTs `/xagent/start`.

3. **`WarmCache` struct** — Add the `WarmCache` (schedule / claim / pending /
   flush) with its own tests, independent of the backend. Delivers: the
   deferred-suspend state machine in isolation. Depends on: nothing. Verifiable
   by: `WarmCache` unit tests with an injected `suspend` func and a fake clock —
   `Schedule` fires after the window, `Claim` wins before it and loses after,
   `Pending` tracks liveness, `Flush` suspends all now, and exactly one terminal
   transition per entry under concurrency.

4. **Wire `WarmCache` into `Backend`** — Replace the inline suspend in `Wait`
   with `b.warm.Schedule`, make `Probe` report `b.warm.Pending` VMs as
   `StateExited`, `Flush` on `Close`, and `Claim` on `Destroy`. Delivers: `Wait`
   returns promptly and the suspend is deferred by the configured delay. Depends
   on: (1) and (3). Verifiable by: backend unit tests against the `Cloud`/`Stager`
   fakes — driver-exit schedules (not fires) the suspend, it fires after the
   window, `Probe` is `StateExited` during the window, `Close` flushes, and
   `delay == 0` suspends inline as today.

5. **Warm claim in `Launch(reuse)`** — Have `tryResume` `Claim` a pending
   warm entry (skipping `ResumeMicrovm`) and re-spawn through the same
   `/xagent/start` the cold path already uses, falling through to the cold branch
   when the suspend won the race. Delivers: end-to-end warm reuse. Depends on: (2)
   and (4). Verifiable by: backend unit tests — a reuse during the window skips
   the suspend and calls **no** `ResumeMicrovm` (but still POSTs `/xagent/start`);
   a reuse after the window resumes cold; a reuse racing the fired suspend
   resolves to exactly one outcome.

## Trade-offs

- **Detached suspend vs. blocking `Wait`.** The issue frames the delay as living
  "inside `Wait`." Blocking there is simpler but cannot deliver the feature: it
  pins a concurrency slot for the whole window (see Design), so a busy runner
  starves the very follow-up the linger exists to serve, and the follow-up then
  resumes cold. The detached-timer design costs an in-memory registry and a
  small `Probe`/`Close` change but is the only shape that actually keeps the VM
  reusable. It is also strictly better on resource use: idle lingering VMs no
  longer consume runner concurrency.

- **`WarmCache` struct vs. fields on `Backend`.** The deferred-suspend state
  machine (schedule / claim / flush, each entry exactly one terminal transition
  under concurrency) is the subtlest part of the design. Housing it in its own
  `WarmCache` — with the actual `SuspendMicrovm` call injected as a `suspend
  func(id)` — lets it be unit-tested against a fake clock with no `Backend`,
  `Cloud`, or `Stager`, and keeps `Backend`'s methods thin delegations. The cost
  is one extra type and a small indirection; the payoff is that the race-prone
  logic is isolated and independently verifiable (implementation slice 3).

- **One re-spawn trigger vs. two.** Reusing a `RUNNING` VM needs a driver
  re-spawn without a control-plane resume. Rather than add a *second* spawn
  trigger next to the AWS `/resume` hook (leaving the shim to behave differently
  warm vs. cold), the runner owns the re-spawn uniformly via `/xagent/start` and
  the resume hook becomes a thaw-seam no-op — so the shim genuinely cannot tell
  the two apart. The cost is that this reworks the *existing* cold-resume trigger,
  not just the new warm one: cold resumes now depend on the runner POSTing
  `/xagent/start` (retried over the same proxy reachability the SSE stream already
  needs) instead of the in-VM hook self-spawning. The payoff is a single,
  idempotent code path and a shim with one less thing to know. The rejected
  alternative — suspend on driver-exit and always resume — is today's behavior and
  the thing we are trying to avoid; a "resume a VM we never suspended" API does
  not exist.

- **Cost.** A lingering VM bills compute for up to `suspend_delay_seconds`; a
  suspended VM is snapshot-storage only. The default of `0` keeps the current
  cost profile, and the value is bounded below `max_duration`. Operators opt in
  per workspace and choose a conservative window (a few minutes).

- **Config in `handleData` vs. a live workspace lookup.** Persisting the delay in
  the handle keeps `Backend.Wait`'s signature and the `backend.Backend` interface
  untouched, at the cost of edits applying on next launch rather than
  retroactively. Threading the workspace into `Wait` would be a cross-backend
  interface change for a single backend's knob.

## Open Questions

- **Global cap.** Should the runner enforce a global maximum linger (a flag) that
  clamps the per-workspace value, as a blast-radius bound on cost independent of
  per-workspace config?
- **Token / env freshness on warm respawn.** The warm respawn reuses the retained
  bundle's env (`XAGENT_TOKEN`, etc.), exactly as the existing `/resume` path
  does. If short-lived credentials in the bundle are a concern, it is a
  pre-existing resume issue, not introduced here — but worth confirming it is
  acceptable for the reuse path too.
- **Metrics.** Worth emitting a counter for warm-claim hits vs. cold-resume
  fallbacks so the chosen delay can be tuned against real follow-up latencies?
- **Idle-policy relationship.** The backend still omits `run-microvm`'s
  `idlePolicy` (600s floor, can't express "never auto-suspend"). The
  runner-driven linger keeps suspend authority on our side; no change there, but
  confirm we do not also want a belt-and-suspenders service idle policy as a
  backstop if the runner dies mid-linger before `Close` flushes.
