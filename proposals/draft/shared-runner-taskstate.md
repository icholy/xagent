# Shared Runner-Local Taskstate Store

Issue: https://github.com/icholy/xagent/issues/1075

> This change lands **before** the lambda-microvm backend (PR #1054,
> `feat/lambda-microvm-backend`). It introduces the shared `taskstate` store and
> converts the **Docker** backend to use it now; the in-flight microvm backend
> will **adopt** this store when it lands rather than carrying its own. The
> relationship is forward-looking, not an extraction of existing microvm code.

## Problem

A runner backend has to answer one question for every task it hosts — *which
sandbox belongs to this task, and is it still alive?* — and today the only
backend, **Docker**, answers it by treating the runtime itself as the database.

Container **labels** are the index. `find` is a label-filtered `ContainerList`
on every call; `List` re-lists by `xagent=true`/`xagent.runner=<id>` and parses
the `xagent.task` label, with an error branch for a malformed value; `Watch`
reconstructs the task id and exit code from docker event-attribute strings.
There is no runner-local record of the task→container mapping at all — the daemon
*is* the source of truth.

This has worked because Docker hands back rich, queryable metadata. But it does
not generalize, and the runtime-as-truth approach is about to multiply:

- **More backends are arriving.** The lambda-microvm backend is in review
  (PR #1054), and firecracker / agentcore are proposed
  (`proposals/draft/`). None of them get Docker's label-query convenience. AWS
  assigns a microVM's id and there is *no deterministic name to re-derive*, so
  that backend necessarily keeps a runner-local handle somewhere; firecracker
  and agentcore will face the same. Without a shared answer, **each backend
  invents its own** task→sandbox bookkeeping (the in-flight microvm branch
  already adds a per-task JSON store under
  `internal/runner/backend/lambdamicrovm/taskstate`), and the runner ends up with
  several divergent strategies — *label-as-truth* here, *local-store-as-truth*
  there — that the orchestrator has to reason about uniformly.
- **Discovery and liveness are entangled.** The Docker path mixes its *liveness*
  check with task-id string parsing (`strconv.ParseInt` on a label, an error
  branch for a bad value) that has nothing to do with whether a container is
  running.
- **A naive per-task store has a torn-write trap.** The straightforward
  one-file-per-task JSON approach a new backend reaches for (`os.WriteFile` +
  skip-unreadable-on-`List`) silently drops a tracked task if a crash interrupts
  a write. Solving this once, correctly, beats each backend re-solving it.
- **There is no backend-agnostic place** that records "task N maps to handle H on
  backend B" — which is exactly the mapping every present and future backend
  needs.

Rather than let that divergence set in, introduce the shared mapping **now**,
adopt it in the Docker backend, and give every future backend (microvm first)
one place to plug into.

## Design

### Overview

Introduce a single shared runner-level package, `internal/runner/taskstate`, and
make it the **single source of truth** for the task→sandbox-handle mapping for
**every** backend. The Docker backend adopts it in this change; the
lambda-microvm backend adopts it when #1054 lands; firecracker / agentcore use
it from day one.

The store records, per task, which backend owns the sandbox and an opaque
per-backend **handle** (the container id for Docker; for a microVM it will be
`{microvmId, imageARN, stageBucket, stageKey}`). Crucially, **the runner owns the
store and every mutation of it.** Backends no longer persist, discover, or
reconcile anything: the `Backend` interface is rewritten down to per-sandbox
runtime ops — **launch / probe / signal / destroy** — that take and return
handles, and the runner composes `List` / `Running` / `Remove` / reconcile over
them on top of the store. This is the explicit architectural choice: taskstate is
a *runner* responsibility, not a per-backend one, and the interface change is part
of the design rather than a deferred option.

Five decisions frame the design.

1. **Runner-owned, not the C2.** Sandbox-handle state is a runner concern. The
   server already owns the task's logical state (status, events, links) and is
   deliberately ignorant of *how* a runner sandboxes a task. We do **not** push
   the mapping into the C2: a handle is meaningful only to the runner that
   created it (a container id is local to one daemon; a microVM id to one
   account/region), and persisting it server-side would couple the C2 to backend
   internals it has no reason to know. The store lives on the runner's local
   filesystem.

2. **The store is the only source of truth; tags/labels become informational.**
   Docker container labels (`xagent.task`, `xagent.runner`, `xagent=true`) — and
   the equivalent tags a microVM backend sets — are **kept**, but only for human
   visibility (`docker ps`, the AWS console, ad-hoc `aws lambda list-microvms`).
   They are **never read** for discovery or for any lifecycle decision. This is
   the decisive simplification: today the Docker backend's correctness *depends*
   on the daemon faithfully returning the right labels; after this change the
   labels are decoration and the store is authority.

3. **Backing store: atomic per-task JSON files.** One
   `<state-dir>/tasks/<id>.json` per task, with an **atomic** `Write`: marshal to
   a temp file in the same directory, `fsync`, then `os.Rename` over the target
   (rename is atomic on a single filesystem). This closes the torn-write window
   where `List` would skip a half-written file and a tracked task vanishes —
   designed in from the start rather than discovered later. Explicitly **no
   sqlite / no `modernc.org/sqlite`** dependency: the runner binary is baked into
   every container/microVM image at `backend.BinaryPath`, and one-file-per-task
   JSON with atomic rename is sufficient for the handful of concurrent tasks a
   single runner hosts. A directory of small JSON files is also trivially
   inspectable and recoverable by hand, which matters for an
   operability-sensitive runner.

4. **A generic, backend-agnostic record.** The store persists:

   ```go
   // Record is the runner's authoritative task→sandbox mapping. The runner
   // writes it; Handle is an opaque blob the owning backend produced and is the
   // only code that interprets.
   type Record struct {
       TaskID  int64           `json:"task_id"`
       Backend string          `json:"backend"` // "docker", "lambda-microvm", ...
       Handle  json.RawMessage `json:"handle"`  // backend-defined shape
   }
   ```

   Each backend defines its own handle type and (un)marshals it:

   ```go
   // docker (this change)
   type handle struct {
       ContainerID string `json:"container_id"`
   }

   // lambda-microvm (when #1054 adopts the store)
   type handle struct {
       MicrovmID   string `json:"microvm_id"`
       ImageARN    string `json:"image_arn"`
       StageBucket string `json:"stage_bucket"`
       StageKey    string `json:"stage_key"`
   }
   ```

   The store stays backend-agnostic: it persists, lists, and removes records
   without ever decoding `Handle`. The `Backend` field lets a single store
   directory safely hold records from a runner that advertises more than one
   backend, and lets `List` skip records that don't belong to the active backend.

5. **The runner owns taskstate mutations; the `Backend` interface is rewritten.**
   The `Backend` interface today mixes runtime ops with discovery and persistence
   (`Start`/`Stop`/`Running`/`List`/`Remove`/`Watch`). It is replaced by a
   handle-oriented interface that does runtime work only and never touches the
   store:

   ```go
   // Handle is an opaque, backend-defined blob. The runner stores it verbatim
   // in Record.Handle and hands it back; only the owning backend decodes it.
   type Handle = json.RawMessage

   type Backend interface {
       ValidateWorkspace(ws *workspace.Workspace) error

       // Launch ensures a sandbox exists for the spec and starts it, returning
       // the handle the RUNNER persists. If reuse is non-nil, the backend may
       // adopt the sandbox it identifies (preserving its filesystem) instead of
       // creating fresh, or clean up its stale resources. The backend performs
       // no persistence of its own.
       Launch(ctx context.Context, spec *Spec, reuse Handle) (Handle, error)

       // Probe reports the liveness of a single handle.
       Probe(ctx context.Context, h Handle) (State, error)

       // Signal gracefully stops the sandbox identified by h (SIGTERM →
       // SIGKILL / terminate-microvm), reporting whether a running sandbox was
       // signalled — the driver then owns the terminal event report.
       Signal(ctx context.Context, h Handle) (signalled bool, err error)

       // Destroy deletes the sandbox identified by h.
       Destroy(ctx context.Context, h Handle) error

       // Watch streams sandbox exits keyed by handle until ctx is cancelled.
       // The runner resolves handle→task and dedups; Watch performs no
       // persistence. (Stays per-backend — see below.)
       Watch(ctx context.Context, handle func(HandleExit)) error

       Close() error
   }

   type HandleExit struct {
       Handle   Handle
       ExitCode int
   }
   ```

   `Spec`, `State` (`StateRunning`/`StateExited`/`StateUnknown`), and the
   orchestrator's `Sandbox`/`Exit` task-keyed views are unchanged; what moves is
   *who* maps task↔handle. The runner (`runner.Runner`) gains a
   `*taskstate.Store` and becomes the only writer:

   ```
   // runner-side, composed over the backend + store
   Start(taskID, spec):
       rec, ok := store.Read(taskID)
       var reuse Handle
       if ok {
           if backend.Probe(rec.Handle) == StateRunning { return }   // idempotent
           reuse = rec.Handle                                        // adopt / cleanup
       }
       h := backend.Launch(spec, reuse)
       store.Write(Record{taskID, backendName, h})                  // ← runner owns the mutation

   List():     for rec in store.List(backendName):
                   emit Sandbox{rec.TaskID, backend.Probe(rec.Handle)}
   Running(t): backend.Probe(store.Read(t).Handle) == StateRunning
   Remove(t):  backend.Destroy(store.Read(t).Handle); store.Remove(t)
   Kill(t):    backend.Signal(store.Read(t).Handle)
   ```

   So the orchestrator's `Reconcile`/`Prune`/`Monitor` no longer call a backend
   `List`/`Remove`/`Watch` that hides a store; they call these runner-owned
   wrappers, and the store mutation lives in exactly one place.

   - **Docker's `Launch`** inspects `reuse` (a `{container_id}`): if the container
     exists it reuses it (`RepairNetworks` + `ContainerStart`), else it creates
     `xagent-{taskID}`; on a name conflict it adopts the existing container by
     name and returns its id. **`Probe`** is `ContainerInspect(containerID)`
     (`running`→`StateRunning`, `exited`/`dead`/not-found→`StateExited`),
     replacing the label-filtered `ContainerList` in `find` and the
     label-parsing loop in `List`. No Docker code parses `xagent.task` anymore.
   - **A microVM backend** (future) implements `Launch` (stage bundle +
     `RunMicrovm`; `reuse` carries the prior handle so the backend can delete the
     stale staged object — exactly the in-flight branch's `Start` stale-handle
     cleanup) and `Probe` as a lookup in one `ListMicrovms` call (its current
     `microvmsByID` reconcile). It implements only these four methods and inherits
     List/Running/Remove/reconcile from the runner.

   A future refinement is a batched `ProbeAll(ctx, []Handle) []State` so a
   list-based backend answers a whole `List` in one `ListMicrovms` call rather
   than N probes; the single-handle `Probe` is the floor (see Open Questions).

### `Watch` stays per-backend

`Watch` is the one runtime op that is **not** collapsed into the
launch/probe/signal/destroy quartet, and it stays per-backend. Docker keeps its
push-based `die` event stream (`docker.Events` filtered to this runner's
containers); a microVM backend keeps its `ListMicrovms` poll loop. The reasons the
two differ are intrinsic — Docker has a real event bus, Lambda does not — and
forcing a common abstraction would either throw away Docker's push semantics or
impose a poll on a backend that doesn't need one.

What *does* change is that `Watch` now reports exits keyed by **handle**, not by a
task id the backend parsed out of runtime metadata. The runner resolves
handle→task via the store (a small reverse index) and applies the same
dedup/enqueue logic `Monitor` has today. So Docker's `Watch` stops doing
`strconv.ParseInt` on `xagent.task` event attributes, and a microVM `Watch` no
longer needs to read the store itself — it just emits the handles it sees go
terminal and the runner ignores any it doesn't track.

### Package layout

```
internal/runner/
├── taskstate/                  new shared, atomic-write store (imported by runner.go, NOT by backends)
│   └── taskstate.go            Record{TaskID, Backend, Handle}; Write/Read/Remove/List/ByHandle
├── runner.go                   owns *taskstate.Store; composes List/Running/Remove/reconcile over the backend
└── backend/
    ├── backend.go              rewritten Backend interface (Launch/Probe/Signal/Destroy/Watch)
    └── docker/                 implements the four ops; handle = {container_id}
```

Backends no longer import `taskstate` at all — only `runner.go` does. When the
lambda-microvm backend lands, it adds `backend/lambdamicrovm/` implementing the
same four ops with a `{microvm_id, ...}` handle, and drops both the standalone
`backend/lambdamicrovm/taskstate` package *and* its own List/Watch store-scanning
its branch currently carries.

### Rollout for the Docker backend

This is the only backend that changes on disk today, and the transition is
deliberately conservative:

- **New state dir.** The runner gains a per-runner state directory for the Docker
  backend (e.g. `/var/lib/xagent/docker/<runner-id>/tasks/<id>.json`). On first
  run after upgrade the store is empty.
- **The runner writes the record.** After `backend.Launch` returns a handle, the
  runner writes the `{container_id}` record; on `Remove` the runner calls
  `backend.Destroy` and deletes the record. The backend itself persists nothing.
- **Pre-existing containers.** Containers created by the old (label-only) code
  before the upgrade are not in the store, so the first `List` won't see them.
  This is benign in practice because the container name `xagent-{taskID}` is
  deterministic: the next `Start` for such a task calls `Launch` with no `reuse`,
  hits the existing name, adopts the running container, and the runner records it
  — self-healing the gap. (See the first trade-off for the general statement.)

## Trade-offs

**Tags are informational, so a wiped state dir loses live sandboxes.** Because
nothing is ever discovered from labels/tags, a *fresh* runner with an empty or
deleted `tasks/` directory will **not** rediscover sandboxes that a prior
instance started on a shared daemon/account. This is the cost of decision #2 and
it is **accepted**: the state dir is persistent across same-host restarts (it
lives on the host, not in the sandbox), which covers the real recovery case (a
runner process restart re-reads its own `tasks/`). Cross-host failover or a
deliberately wiped state dir is **out of scope** — recovering that would require
re-opening tag-based discovery, which is exactly what this change removes. (An
optional boot-time reseed is listed as an Open Question.)

**Create-then-record orphan gap.** There is a window between creating a sandbox
and writing its record. A crash in that window orphans an untracked sandbox. How
exposed a backend is depends on whether its handle is deterministic:

- **Docker is self-protected.** The container name `xagent-{taskID}` is
  *deterministic*. A lost store-write after `Launch` created the container means
  the next `Start` calls `Launch` again (with no `reuse` handle, since the record
  was lost) and hits a **name conflict**, which *surfaces* the orphan instead of
  silently spawning a duplicate. `Launch` resolves the conflict by adopting the
  existing container (inspect by name, return its id) and the runner re-records
  it, turning the gap into a self-heal. This is why the Docker adoption in this
  change is safe even though the runner's store-write can lag the create.
- **A microVM backend will not be self-protected.** Its id is **AWS-assigned**
  and only known *after* `RunMicrovm` returns, so `Launch` can only return the
  handle once the VM exists and the runner's record is necessarily written
  *after* that. A crash between `Launch` returning the handle and the runner's
  store-write succeeding orphans a running, untracked microVM — and with tags no
  longer a discovery fallback (precisely the open question the microvm proposal
  raises), nothing will ever find it again. The only backstop is
  `max_duration_seconds` (≤ 8 h, surfaced as billing): the VM self-expires. That
  backend will lean on `max_duration` as the reaper. This is called out here
  because it is a direct consequence of decision #2 that the microvm backend
  inherits when it adopts the store — not a property of the Docker change.

**`RepairNetworks` is not removed by this change.** It would be tempting to read
this as "stop using the daemon as truth ⇒ delete the messy Docker reconcile
code." But `RepairNetworks` exists because the Docker backend **reuses** a task's
container across restarts (to preserve its filesystem), and a reused container's
network endpoint id drifts after `docker compose down && up`. That complexity
comes from container *reuse*, not from label-based discovery, so it is orthogonal
and stays exactly as is. This change removes label *parsing*, not container
reuse.

**One shared store directory vs. per-backend dirs.** A runner that advertises
multiple backends could collide task ids across backends in one `tasks/` dir. The
`Record.Backend` field disambiguates and `List` filters to the active backend, so
a single directory is safe and keeps the path scheme uniform. The alternative (a
per-backend subdirectory) is also viable but buys little once records are
self-describing.

## Open Questions

1. **Boot-time tag reseed as an opt-in recovery path.** Should the shared store
   optionally **seed/repair itself from runtime tags at startup** — re-opening
   the discovery fallback *only at boot* — to cover the wiped-dir / cross-host
   case (and, for a microVM backend, the orphan gap), while staying store-only
   during steady-state? This would be a single `reseed()` that lists by tag once
   on start and writes any missing records, then never consults tags again. It
   restores resilience at the cost of re-introducing the tag dependency the
   design just removed (and needs a tag→handle reconstruction per backend).
   Recommendation: ship **store-only** first; add reseed behind a flag if the
   orphan/failover cases prove painful in practice.

2. **Residual `Backend` interface signature details.** The *shape* is decided —
   the runner owns the store and the interface is the handle-oriented
   Launch/Probe/Signal/Destroy/Watch quartet above (the former "keep the current
   interface" option is rejected per maintainer direction). What remains is the
   exact signatures:
   - **`reuse Handle` on `Launch` vs. a separate `Adopt`.** Folding "create
     fresh" and "reuse/cleanup the prior sandbox" into one `Launch(spec, reuse)`
     keeps the runner's `Start` to a single call, but overloads `Launch` with two
     intents. The alternative is an explicit `Adopt(handle) (State, error)` the
     runner calls before deciding to reuse. Recommend the `reuse` parameter — it
     matches both Docker reuse and the microVM stale-cleanup with one method.
   - **Single `Probe` vs. batched `ProbeAll`.** Ship `Probe(h) (State, error)` as
     the floor; add `ProbeAll([]Handle) ([]State, error)` if/when a list-based
     backend (microVM) makes N-probes-per-`List` a measurable cost. The runner's
     `List` can prefer `ProbeAll` when the backend implements it.
   - **`Watch` keyed by `HandleExit` vs. the runner supplying the handle set.**
     Proposed: the backend emits terminal handles it observes and the runner
     resolves/filters via the store. Should the runner instead pass the tracked
     handle set into `Watch` so a polling backend only lists what it must? Leaning
     to the former (keeps the backend stateless) unless poll cost argues
     otherwise.

3. **Coordination with the in-flight microvm backend (#1054).** Since this lands
   first, #1054 will be updated to use `internal/runner/taskstate` with a
   `{microvm_id, ...}` handle and drop its standalone
   `backend/lambdamicrovm/taskstate` package. Its on-disk path
   (`tasks/<id>.json`) is unchanged; only the record shape gains the
   `backend`/`handle` envelope. Should that conversion happen *in* #1054 (rebased
   on this), or as a follow-up immediately after both land? No production microvm
   store exists yet, so there is no data migration either way — purely a
   merge-ordering choice for the maintainer.
