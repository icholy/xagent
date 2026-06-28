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
`{microvmId, imageARN, stageBucket, stageKey}`). Backends stop discovering
sandboxes from the runtime; instead they iterate the store and run a narrow
per-handle **liveness probe** against the runtime.

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
   // Record is the runner's authoritative task→sandbox mapping. Handle is an
   // opaque, backend-owned blob: the backend that wrote it is the only code
   // that interprets it.
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

5. **Reconcile collapses to "store entries × a per-handle probe".** This is the
   payoff. Instead of a backend hand-rolling List/Running/discovery, there is one
   shared shape:

   ```
   List():     for each record in store.List(filtered to this backend):
                   state := probe(record.Handle)        // Running | Exited | Unknown
                   emit Sandbox{record.TaskID, state}
   Running(t): probe(store.Read(t).Handle) == Running
   ```

   where each backend implements only:

   ```go
   // probe reports the liveness of a single handle. It is the entire
   // runtime-specific surface of discovery.
   probe(ctx, handle) (backend.State, error)
   ```

   - **Docker's probe** (this change) is a `ContainerInspect(containerID)`:
     `running` → `StateRunning`, `exited`/`dead` → `StateExited`, not-found →
     `StateExited`. This *replaces* the per-call label-filtered `ContainerList`
     in `find` and the label-parsing loop in `List`. No Docker code parses
     `xagent.task` anymore.
   - **A microVM probe** (future) is a lookup into one `ListMicrovms` call: alive
     → `StateRunning`, terminal/absent → `StateExited`. The in-flight backend's
     `Start` stale-handle cleanup and its `microvmsByID` reconcile route through
     this same shared path when it adopts the store.

   Because a single `ListMicrovms`-style call can answer many handles at once, the
   shared `List` lets a backend optionally **batch** the probe: the wrapper hands
   the backend the full set of handles and the backend returns states in one shot
   (Docker inspects one-by-one; a list-based backend lists once). See Open
   Questions for the exact interface shape.

   Future backends (firecracker, agentcore) then implement only
   **launch / probe / signal / destroy** and inherit List/Running/reconcile for
   free.

### `Watch` stays per-backend

`Watch` is **not** unified. Docker keeps its push-based `die` event stream
(`docker.Events` filtered to this runner's containers); a microVM backend keeps
its `ListMicrovms` poll loop. The reasons the two differ are intrinsic — Docker
has a real event bus, Lambda does not — and forcing a common abstraction would
either throw away Docker's push semantics or impose a poll on a backend that
doesn't need one. `Watch` *does* benefit indirectly: Docker's `Watch` can stop
parsing the task id out of event attributes and instead look the container id up
in the store (`store.byHandle`) — but the loop structure stays backend-specific.

The orchestrator (`runner.Runner`) is **untouched**: `Reconcile`, `Prune`, and
`Monitor` still call `backend.List` / `backend.Watch` and consume
`[]backend.Sandbox` / `backend.Exit` exactly as today. This change lives
entirely under the `backend` seam.

### Package layout

```
internal/runner/
├── taskstate/                  new shared, atomic-write store
│   └── taskstate.go            Record{TaskID, Backend, Handle}; Write/Read/Remove/List
├── runner.go                   unchanged orchestrator
└── backend/
    ├── backend.go              Backend interface (see Open Questions)
    └── docker/                 adopts the store; handle = {container_id}; probe = ContainerInspect
```

When the lambda-microvm backend lands, it adds `backend/lambdamicrovm/` using the
same store with a `{microvm_id, ...}` handle, and drops the standalone
`backend/lambdamicrovm/taskstate` package its branch currently carries.

### Rollout for the Docker backend

This is the only backend that changes on disk today, and the transition is
deliberately conservative:

- **New state dir.** The Docker backend gains a per-runner state directory
  (e.g. `/var/lib/xagent/docker/<runner-id>/tasks/<id>.json`). On first run after
  upgrade the store is empty.
- **`Start` writes the record.** After `ContainerCreate` (or after adopting an
  existing container by name — see below), `Start` writes the `{container_id}`
  handle. `Remove` deletes the record alongside the container.
- **Pre-existing containers.** Containers created by the old (label-only) code
  before the upgrade are not in the store, so the first `List` won't see them.
  This is benign in practice because the container name `xagent-{taskID}` is
  deterministic: the next `Start` for such a task inspects the name, adopts the
  running container, and records it — self-healing the gap. (See the first
  trade-off for the general statement.)

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
  *deterministic*. A lost store-write after `ContainerCreate` means the next
  `Start` for that task tries to create `xagent-{taskID}` again and hits a
  **name conflict**, which *surfaces* the orphan instead of silently spawning a
  duplicate. `Start` resolves the conflict by adopting the existing container
  (inspect by name, re-record), turning the gap into a self-heal. This is why
  the Docker adoption in this change is safe even though the store can lag a
  create.
- **A microVM backend will not be self-protected.** Its id is **AWS-assigned**
  and only known *after* `RunMicrovm` returns, so its record is necessarily
  written *after* the VM exists. A crash between the run call returning and the
  store write succeeding orphans a running, untracked microVM — and with tags no
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

2. **Exact `Backend` interface impact.** Two shapes:

   - **(a) Shrink the interface to per-handle ops.** Replace `Start`/`Stop`/
     `Running`/`List`/`Remove` discovery with `Launch(spec) (handle, error)`,
     `Probe(handle) (State, error)` (or `ProbeAll([]handle) []State` for
     batching), `Signal(handle)`, `Destroy(handle)`, and let a **shared wrapper**
     in `package backend` own the store and implement `List`/`Running`/`Remove`/
     `Reconcile` on top. Future backends implement four small methods. This is
     the larger, cleaner refactor and the one that delivers the "new backends
     implement only launch/probe/signal/destroy" promise — but it churns the
     interface and the one existing backend + its moq.
   - **(b) Keep the current `Backend` interface, share only a helper.** Backends
     keep `Start/Stop/Running/List/...`, but each delegates to a shared
     `taskstate.Store` and a shared `Reconcile(store, probe)` helper. Smaller
     diff, no interface churn, but each backend still writes a thin List/Running
     that calls the helper.

   **Recommendation: start with (b) in this change** (Docker is the only backend,
   and designing the per-handle interface from Docker alone risks baking in
   container-shaped assumptions), then move to **(a)** once the microvm backend
   gives a second real implementation to validate the seams against. (b) is
   forward-compatible with (a).

3. **Coordination with the in-flight microvm backend (#1054).** Since this lands
   first, #1054 will be updated to use `internal/runner/taskstate` with a
   `{microvm_id, ...}` handle and drop its standalone
   `backend/lambdamicrovm/taskstate` package. Its on-disk path
   (`tasks/<id>.json`) is unchanged; only the record shape gains the
   `backend`/`handle` envelope. Should that conversion happen *in* #1054 (rebased
   on this), or as a follow-up immediately after both land? No production microvm
   store exists yet, so there is no data migration either way — purely a
   merge-ordering choice for the maintainer.
