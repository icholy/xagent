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
the re-spawn trigger uniformly: a single `POST /xagent/run` on the control
surface is the *only* way the driver is re-started, and the shim cannot tell
which path led there. This retires the one place the shim currently distinguishes
them — the AWS `/resume` hook re-spawning on its own.

The runner POSTs `/xagent/run` **only on the driver-is-dead paths** (a warm claim
or a cold resume — in both, the previous driver has exited). It is never sent to
adopt a VM whose driver is still alive. That makes "a driver is already running"
an **invariant violation**, so `/xagent/run` **errors (409)** on it rather than
silently no-op'ing: a spurious re-run is a runner bug and should surface loudly,
not be swallowed.

**Shim: one re-spawn entry point, resume hook demoted to a thaw seam.** Today
`resumeHook` re-spawns the driver (`resumeHook` → `spawn`). Move that trigger to
the control surface and make the resume hook a pure thaw seam — symmetric with
`suspendHook`, which is already a pure flush seam:

```go
const lambdamicrovmRunPath = "/xagent/run"

func (s *Server) ControlHandler() http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("GET "+lambdamicrovmLifecyclePath, s.lifecycleHandler)
    mux.HandleFunc("POST "+lambdamicrovmStopPath, s.stopHandler)
    mux.HandleFunc("POST "+lambdamicrovmRunPath, s.runHandler)
    return mux
}

// ShimResponseHeader marks a response as the shim's own word rather than one the
// AWS managed proxy generated before the request reached the shim. The runner
// treats only marked responses as authoritative (see respawn).
const ShimResponseHeader = "X-Xagent-Shim"

// runHandler (re)spawns the driver from the retained bundle. It is the sole
// re-spawn trigger — used identically after a cold ResumeMicrovm and a warm
// reclaim — so the shim never distinguishes the two. It errors (409) if a driver
// is already running or the VM was never provisioned: the runner only calls it
// on a dead driver, so either is a runner-side bug worth surfacing. Every
// response carries the shim marker so the runner can tell it apart from a
// proxy-generated error.
func (s *Server) runHandler(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set(ShimResponseHeader, "1")
    s.mu.Lock()
    running := s.current != nil && !isDone(s.current)
    bundle, started := s.bundle, s.started
    s.mu.Unlock()
    if !started {
        http.Error(w, "not provisioned", http.StatusConflict)
        return
    }
    if running {
        http.Error(w, "driver already running", http.StatusConflict)
        return
    }
    if err := s.spawn(bundle); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    w.WriteHeader(http.StatusOK)
}

// resumeHook becomes a thaw seam: the VM un-snapshots, but the driver is
// re-spawned by the runner's /xagent/run, not here.
func (s *Server) resumeHook(context.Context, awsmicrovm.ResumeHookRequest) error {
    return nil
}
```

The shim still legitimately separates *first provisioning* (the AWS `/run` hook —
fetch bundle, provision files once, first spawn) from *re-spawn* (`/xagent/run` —
retained bundle, no re-fetch). That is a provisioning distinction, not a warm/cold
one; the two live on different surfaces (the hook port vs. the ingress control
port) and share the "run the driver" name. `spawn` already clears the sticky
`driver-exited` (`s.lc.reset()`), so the fresh `Wait` the runner starts blocks for
the *new* run's exit rather than replaying the old one.

**Runner: `tryResume` re-spawns the same way on every dead-driver path.**
`tryResume` posts `/xagent/run` **iff** the driver is dead — after a warm claim or
a cold resume — and never when adopting a VM whose driver is still alive:

```go
func (b *Backend) tryResume(ctx context.Context, reuse *backend.Handle) (bool, backend.Handle, error) {
    hd, _ := decodeData(reuse.Data)

    runDriver := false
    if b.warm.Claim(reuse.ID) {
        // Warm: won the race against the suspend timer; the driver exited (that
        // is what scheduled the suspend) but the VM never left RUNNING, so skip
        // ResumeMicrovm entirely and re-run the driver.
        runDriver = true
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
            // Adopt a VM whose driver is still alive (a restart that beat the
            // suspend, or an externally-resumed VM). Nothing to run — posting
            // /xagent/run here would (correctly) 409 against the live driver.
        case awsmicrovm.MicrovmStateSuspended:
            if _, err := b.cloud.ResumeMicrovm(ctx, &awsmicrovm.ResumeMicrovmInput{MicrovmID: reuse.ID}); err != nil {
                return false, backend.Handle{}, fmt.Errorf("resume microvm: %w", err)
            }
            runDriver = true // un-snapshotted; the driver is dead, re-run it
        default: // SUSPENDING / TERMINATING / TERMINATED
            return false, backend.Handle{}, nil
        }
        if out.Microvm.Endpoint != "" {
            hd.Endpoint = out.Microvm.Endpoint
        }
    }

    if runDriver {
        // The one uniform re-spawn trigger — identical for warm and cold.
        if err := b.respawn(ctx, reuse.ID, hd.Endpoint); err != nil {
            return false, backend.Handle{}, fmt.Errorf("run driver: %w", err)
        }
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

`respawn` mints a proxy token and POSTs `/xagent/run` over the managed proxy. The
subtlety here — raised in review — is that **the AWS proxy sits in front of the
shim and can produce an HTTP response of its own before the request ever reaches
the shim**: a `401/403` for an expired/invalid auth token, a `5xx`/`429` while the
VM is still coming up or the proxy is throttling, a `404` for an unknown endpoint.
So a status code alone is not trustworthy as "the shim's answer." `respawn`
therefore classifies responses the same way the rest of the backend already treats
the proxy — reachability/auth failures are retryable, never terminal — and only a
response bearing the shim marker (`X-Xagent-Shim`) is authoritative:

- **Transport error, or any response *without* `X-Xagent-Shim`** (proxy `5xx` /
  `429` / `404`, etc.) → the request did not reach the shim: back off and retry,
  re-minting the token on `401/403` (as `Wait` already does) and consulting
  `GetMicrovm` to bail out if the VM has gone terminal (mirroring `arbitrate`).
- **Shim-marked `200`** → the driver was spawned; done.
- **Shim-marked `409`** → the invariant was violated (a driver was already
  running); surface it as a runner bug.

Because only a shim-marked response ends the retry loop and the runner sends
`/xagent/run` exactly once per dead-driver path, one intended run never fans out
into two. (The marker's pass-through behavior — the proxy forwarding shim response
headers on success and *not* fabricating them on its own errors — is an assumption
to confirm against real AWS, alongside the existing #1088 proxy validation.)

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

2. **Unify the shim re-spawn trigger** — Add `runHandler` (`POST /xagent/run`) as
   the sole driver re-spawn entry point — 200 on spawn, **409 if a driver is
   already running** or the VM was never provisioned, every response stamped with
   the `X-Xagent-Shim` marker — and demote `resumeHook` to a thaw-seam no-op.
   `tryResume` POSTs `/xagent/run` on each dead-driver path (cold resume today;
   warm claim in slice 5) via a `respawn` helper that treats only shim-marked
   responses as terminal and retries proxy/transport/auth failures (re-mint token,
   `GetMicrovm` bail-out), so the cold-resume path re-spawns the same way the warm
   path will. Delivers: one uniform re-spawn path the shim can't distinguish; the
   existing cold resume keeps working through it. Depends on: nothing new (reworks
   the existing resume path). Verifiable by: shim unit tests — after a driver exit
   `POST /xagent/run` re-spawns and publishes a fresh `driver-exited`, a POST while
   running is a marked 409, a POST before provisioning is a marked 409, and the
   resume hook alone no longer spawns; `respawn` unit tests — an unmarked
   proxy-shaped `5xx`/`401` retries (re-mints), a marked `200` succeeds, a marked
   `409` errors; backend test that a cold resume POSTs `/xagent/run`.

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
   `/xagent/run` the cold path already uses, falling through to the cold branch
   when the suspend won the race. Delivers: end-to-end warm reuse. Depends on: (2)
   and (4). Verifiable by: backend unit tests — a reuse during the window skips
   the suspend and calls **no** `ResumeMicrovm` (but still POSTs `/xagent/run`);
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
  warm vs. cold), the runner owns the re-spawn uniformly via `/xagent/run` and the
  resume hook becomes a thaw-seam no-op — so the shim genuinely cannot tell the
  two apart. The cost is that this reworks the *existing* cold-resume trigger, not
  just the new warm one: cold resumes now depend on the runner POSTing
  `/xagent/run` (retried over the same proxy reachability the SSE stream already
  needs) instead of the in-VM hook self-spawning. The payoff is a single code path
  and a shim with one less thing to know. The rejected alternative — suspend on
  driver-exit and always resume — is today's behavior and the thing we are trying
  to avoid; a "resume a VM we never suspended" API does not exist.

- **`/xagent/run` errors on a live driver vs. idempotent no-op.** The runner only
  posts `/xagent/run` on a dead-driver path (warm claim or cold resume), never to
  adopt a live driver, so "a driver is already running" is an invariant violation
  and the shim 409s rather than swallowing it — surfacing the runner bug instead
  of hiding it. This costs the blind retry-safety a no-op would give, which is why
  `respawn` distinguishes a shim-authoritative response (marked `X-Xagent-Shim`,
  terminal) from a proxy-generated one (retryable): only a shim `200`/`409` ends
  the loop, so a single intended run never becomes two. The one residual edge — a
  shim `200` whose ack is lost, whose retry then hits a proxy/`409` — is narrow and
  self-correcting (the driver the runner wanted *is* running); it is called out
  below rather than papered over with a no-op.

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
- **Proxy vs. shim response marker.** The design leans on the AWS proxy
  forwarding the shim's `X-Xagent-Shim` header on success and not fabricating it on
  its own errors, so the runner can tell a shim `200`/`409` from a proxy `5xx`/
  `401`. This needs validating against real AWS (as with the #1088 proxy work). If
  the proxy strips arbitrary response headers, the fallback is to key off the
  proxy's documented status-code conventions (retry `401/403/5xx/429`, treat a
  `409` with the shim's JSON body as authoritative) — less clean, so the marker is
  preferred if it survives the proxy.
- **`/xagent/run` lost-ack handling.** In the narrow window where the shim's `200`
  is produced but its ack is lost, the retry re-posts and finds the driver already
  running. Should the runner treat a shim `409` that follows a prior send as
  "already running ⇒ proceed" (the driver it wanted is up) while still logging it,
  or fail the reuse and let the normal Start machinery re-drive? Leaning toward the
  former, but calling it out.
- **Idle-policy relationship.** The backend still omits `run-microvm`'s
  `idlePolicy` (600s floor, can't express "never auto-suspend"). The
  runner-driven linger keeps suspend authority on our side; no change there, but
  confirm we do not also want a belt-and-suspenders service idle policy as a
  backstop if the runner dies mid-linger before `Close` flushes.
