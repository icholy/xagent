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

### Detached suspend + in-memory linger registry

Add an in-memory registry to `Backend`, keyed by MicroVM id (which is 1:1 with a
task):

```go
type lingerEntry struct {
    mu       sync.Mutex
    done     bool          // scheduled → exactly one of {suspended, claimed, cancelled}
    cancel   context.CancelFunc
}

type Backend struct {
    // ...existing fields...
    lingerCtx context.Context    // backend-lifetime ctx (New → Close), so a
    lingerStop context.CancelFunc // detached suspend outlives the supervise ctx
    mu      sync.Mutex
    linger  map[string]*lingerEntry // microvmID → pending suspend
}
```

**`Wait` change.** On a clean `driver-exited`, instead of suspending inline,
`Wait` schedules a detached suspend and returns immediately:

```go
if exited {
    b.scheduleSuspend(id, suspendDelay) // no-op immediate-suspend when delay == 0
    return backend.ExitCode(code), nil
}
```

`scheduleSuspend` registers a `lingerEntry` and starts a goroutine on
`b.lingerCtx` (not `ctx` — the supervise ctx dies as soon as `Wait` returns) that
waits `suspendDelay`, then transitions the entry to `suspended` under its mutex
and calls `SuspendMicrovm`. If `suspendDelay == 0` it suspends immediately and
skips the registry entirely — byte-for-byte the current behavior, so the feature
is inert until configured.

Because the entry is registered *before* `Wait` returns, it exists before any
follow-up `Start` can observe the VM, closing the ordering gap.

**`Probe` change.** A VM with a pending `lingerEntry` reports `StateExited`
(husk-preserved) even though `GetMicrovm` says `RUNNING`, so the runner's
`Running()` guard does not short-circuit the follow-up and instead drives
`Start` → `Probe` `StateExited` → `Launch(reuse)`:

```go
func (b *Backend) Probe(ctx context.Context, h backend.Handle) (backend.State, error) {
    if b.isLingering(h.ID) {
        return backend.StateExited, nil // warm husk: drive reuse, not "already running"
    }
    // ...existing GetMicrovm switch...
}
```

**`Close` change.** `Close` cancels `lingerCtx` and **flushes** every pending
suspend — i.e. suspends now rather than leaving warm VMs billing compute after
the runner stops. This differs deliberately from the mid-task shutdown contract
(where `Wait` returns without suspending so a *still-running* driver's VM
survives for rehydration): here the driver has already exited, so `SUSPENDED` is
the correct resting state and next boot resumes on demand.

### Warm claim in `Launch(reuse)`

`tryResume` consults the registry before touching the control plane. If the VM
has a pending suspend, it **claims** it — atomically cancels the scheduled
suspend and re-spawns the driver over the proxy, with **no** `ResumeMicrovm`:

```go
func (b *Backend) tryResume(ctx context.Context, reuse *backend.Handle) (bool, backend.Handle, error) {
    if b.claimLinger(reuse.ID) { // won the race against the suspend timer
        hd, _ := decodeData(reuse.Data)
        if err := b.respawn(ctx, reuse.ID, hd.Endpoint); err != nil {
            return false, backend.Handle{}, fmt.Errorf("warm respawn: %w", err)
        }
        return true, backend.Handle{Type: HandleType, ID: reuse.ID, Data: reuse.Data}, nil
    }
    // ...existing GetMicrovm + resume-if-SUSPENDED path (cold fallback)...
}
```

`claimLinger` transitions the entry to `claimed` under its mutex and returns
`true` only if it beat the suspend goroutine. If the suspend already fired
(`done == true` via `suspended`), it returns `false` and execution falls through
to the existing cold-resume path — the window simply elapsed. Exactly one of
{suspend fires, claim wins} completes; the loser is a no-op.

`respawn` POSTs a new shim control endpoint (below). The VM never left `RUNNING`,
so the un-snapshot cost is skipped entirely; only the driver re-spawn remains.

### New shim control endpoint: `POST /xagent/start`

The shim (`internal/microvmshim/microvmshim.go`) already re-spawns the driver
from the retained bundle on the AWS `/resume` hook (`resumeHook` → `spawn`). Add
a sibling to `/xagent/stop` on the ingress control surface that does the same
thing, triggered by the runner over the managed proxy instead of by an AWS
resume:

```go
const lambdamicrovmStartPath = "/xagent/start"

func (s *Server) ControlHandler() http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("GET "+lambdamicrovmLifecyclePath, s.lifecycleHandler)
    mux.HandleFunc("POST "+lambdamicrovmStopPath, s.stopHandler)
    mux.HandleFunc("POST "+lambdamicrovmStartPath, s.startHandler)
    return mux
}

// startHandler re-spawns the driver from the retained bundle for a warm reuse
// (runner-triggered analog of resumeHook). No-op / 409 if a driver is already
// running, so a duplicate claim cannot double-spawn.
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
```

`spawn` already clears the sticky `driver-exited` (`s.lc.reset()`), so the new
`Wait` the runner starts after re-persisting the handle blocks for the *new*
run's exit rather than replaying the old one. The warm respawn reuses the
retained in-VM bundle — identical to `/resume` semantics, where the bundle is
never re-fetched.

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

2. **Shim `/xagent/start` respawn endpoint** — Add `startHandler` to the shim
   control surface, re-spawning the driver from the retained bundle with an
   already-running guard. Delivers: runner-triggered warm respawn. Depends on:
   nothing (independent of the runner). Verifiable by: shim unit tests — after a
   driver exit, `POST /xagent/start` re-spawns and publishes a fresh
   `driver-exited` on the next run; a second POST while running is a no-op; a POST
   before provisioning is a 409.

3. **Detached suspend + linger registry** — Replace the inline suspend in `Wait`
   with `scheduleSuspend` (detached goroutine on a backend-lifetime context),
   register/deregister `lingerEntry`, make `Probe` report a lingering VM as
   `StateExited`, and flush pending suspends on `Close`. `Destroy` deregisters any
   pending suspend for the terminated VM. Delivers: `Wait` returns promptly and
   the suspend is deferred by the configured delay. Depends on: (1). Verifiable
   by: backend unit tests against the `Cloud`/`Stager` fakes — driver-exit
   schedules (not fires) the suspend, the suspend fires after the window, `Probe`
   is `StateExited` during the window, `Close` flushes, and `delay == 0` suspends
   inline as today.

4. **Warm claim in `Launch(reuse)`** — Have `tryResume` claim a pending
   `lingerEntry` (cancelling the suspend) and `respawn` via `POST /xagent/start`
   instead of `ResumeMicrovm`, falling through to the cold-resume path when the
   suspend already won the race. Delivers: end-to-end warm reuse. Depends on: (2)
   and (3). Verifiable by: backend unit tests — a reuse during the window cancels
   the suspend, POSTs `/xagent/start`, and calls **no** `ResumeMicrovm`; a reuse
   after the window resumes cold; a reuse racing the fired suspend resolves to
   exactly one outcome.

## Trade-offs

- **Detached suspend vs. blocking `Wait`.** The issue frames the delay as living
  "inside `Wait`." Blocking there is simpler but cannot deliver the feature: it
  pins a concurrency slot for the whole window (see Design), so a busy runner
  starves the very follow-up the linger exists to serve, and the follow-up then
  resumes cold. The detached-timer design costs an in-memory registry and a
  small `Probe`/`Close` change but is the only shape that actually keeps the VM
  reusable. It is also strictly better on resource use: idle lingering VMs no
  longer consume runner concurrency.

- **Warm respawn vs. suspend/resume anyway.** Reusing a `RUNNING` VM needs a
  driver re-spawn without a control-plane resume, which is why a new shim
  endpoint is required. The alternative — suspend on driver-exit and always
  resume — is today's behavior and the thing we are trying to avoid; a "resume a
  VM we never suspended" API does not exist.

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
