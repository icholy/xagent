# Shared Runner-Local Taskstate Store

Issue: https://github.com/icholy/xagent/issues/1075

> Depends on / builds on **PR #1054** (`feat/lambda-microvm-backend`). The
> `taskstate` package this proposal promotes only exists on that branch; the
> proposal assumes #1054 lands first (or lands together with this change).

## Problem

Every runner backend has to answer the same question — *which sandbox belongs
to which task, and is it still alive?* — and each backend answers it
differently, using the runtime itself as the source of truth.

The **Docker** backend treats container **labels** as its index. `find` is a
label-filtered `ContainerList` on every call; `List` re-lists by
`xagent=true`/`xagent.runner=<id>` and parses the `xagent.task` label, with an
error branch for a malformed value; `Watch` reconstructs the task id and exit
code from docker event-attribute strings. There is no runner-local record of
the task→container mapping at all — the daemon *is* the database.

The **lambda-microvm** backend (PR #1054) had to invent the opposite. AWS
assigns the `microvmId` and there is no deterministic name to re-derive, so the
backend keeps a runner-local JSON store,
`internal/runner/backend/lambdamicrovm/taskstate`, with one `tasks/<id>.json`
per task holding `{microvmId, imageARN, stageBucket, stageKey}`. `List`,
`Running`, and `Watch` all scan that store and reconcile each handle against a
single `ListMicrovms` call (`microvmsByID`).

So we have two divergent strategies — *label-as-truth* vs.
*local-store-as-truth* — and the next two backends on the board (firecracker,
agentcore) will each have to pick one and re-implement the reconcile/list
plumbing again. Concretely:

- Discovery logic (how `List`/`Running` locate a task's sandbox) is duplicated
  and inconsistent across backends.
- The Docker path entangles its *liveness* check with task-id string parsing
  that has nothing to do with liveness.
- The lambda store has a latent torn-write risk: `Write` is a plain
  `os.WriteFile`, and `List` silently skips any unreadable/malformed file — a
  crash mid-write can make a tracked task disappear from `List`.
- There is no single, backend-agnostic place that records "task N maps to handle
  H on backend B" — exactly the mapping every future backend needs.

## Design

### Overview

Promote the lambda-microvm `taskstate` package to a single shared runner-level
package, `internal/runner/taskstate`, and make it the **single source of truth**
for the task→sandbox-handle mapping for **every** backend — Docker and
lambda-microvm today, firecracker/agentcore next.

The store records, per task, which backend owns the sandbox and an opaque
per-backend **handle** (the container id for Docker; `{microvmId, imageARN,
stageBucket, stageKey}` for lambda). Backends stop discovering sandboxes from
the runtime; instead they iterate the store and run a narrow per-handle
**liveness probe** against the runtime.

Five decisions frame the design.

1. **Runner-owned, not the C2.** Sandbox-handle state is a runner concern. The
   server already owns the task's logical state (status, events, links) and is
   deliberately ignorant of *how* a runner sandboxes a task. We do **not** push
   the mapping into the C2: a handle is meaningful only to the runner that
   created it (a container id is local to one daemon; a `microvmId` to one
   account/region), and persisting it server-side would couple the C2 to backend
   internals it has no reason to know. The store lives on the runner's local
   filesystem, exactly where `taskstate` lives today.

2. **The store is the only source of truth; tags/labels become informational.**
   Docker container labels (`xagent.task`, `xagent.runner`, `xagent=true`) and
   MicroVM tags are **kept** — but only for human visibility (`docker ps`, the
   AWS console, ad-hoc `aws lambda list-microvms`). They are **never read** for
   discovery or for any lifecycle decision. This is the decisive simplification:
   today the Docker backend's correctness *depends* on the daemon faithfully
   returning the right labels; after this change the labels are decoration and
   the store is authority.

3. **Backing store: atomic per-task JSON files.** Keep one
   `<state-dir>/tasks/<id>.json` per task (the existing shape), but make `Write`
   **atomic**: marshal to a temp file in the same directory, `fsync`, then
   `os.Rename` over the target (rename is atomic on a single filesystem). This
   closes the torn-write window where `List` silently skips a half-written file
   and a tracked task vanishes. Explicitly **no sqlite / no
   `modernc.org/sqlite`** dependency: the runner binary is baked into every
   container/microVM image at `backend.BinaryPath`, and one-file-per-task JSON
   with atomic rename is sufficient for the handful of concurrent tasks a single
   runner hosts. A directory of small JSON files is also trivially inspectable
   and recoverable by hand, which matters for an operability-sensitive runner.

4. **A generic, backend-agnostic record.** The promoted `State` drops its
   lambda-specific fields and becomes:

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
   // docker
   type handle struct {
       ContainerID string `json:"container_id"`
   }

   // lambda-microvm — exactly today's State minus TaskID
   type handle struct {
       MicrovmID   string `json:"microvm_id"`
       ImageARN    string `json:"image_arn"`
       StageBucket string `json:"stage_bucket"`
       StageKey    string `json:"stage_key"`
   }
   ```

   The store stays backend-agnostic: it persists, lists, and removes records
   without ever decoding `Handle`. The `Backend` field lets a single store
   directory safely hold records from a runner that (per #1054's config story)
   advertises more than one backend, and lets `List` skip records that don't
   belong to the active backend.

5. **Reconcile collapses to "store entries × a per-handle probe".** This is the
   payoff. Today each backend hand-rolls List/Running/reconcile. After the
   change there is one shared shape:

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

   - **Docker's probe** is a `ContainerInspect(containerID)`: `running` →
     `StateRunning`, `exited`/`dead` → `StateExited`, not-found → `StateExited`.
     This *replaces* the per-call label-filtered `ContainerList` in `find` and
     the label-parsing loop in `List`. No backend code parses `xagent.task`
     anymore.
   - **Lambda's probe** is a lookup into one `ListMicrovms` call
     (`microvmsByID`): alive → `StateRunning`, terminal/absent → `StateExited`.
     This is lambda's *existing* `microvmsByID` reconcile, now expressed as the
     shared path. Lambda's `Start` stale-handle cleanup (read prior record →
     `find` → if dead, clean the staged object) also routes through this same
     probe.

   Because a single `ListMicrovms` answers every lambda handle, the shared
   `List` lets a backend optionally **batch** the probe: the wrapper hands the
   backend the full set of handles and the backend returns states in one shot
   (Docker still inspects one-by-one; lambda lists once). See Open Questions for
   the exact interface shape.

   Future backends (firecracker, agentcore) then implement only
   **launch / probe / signal / destroy** and inherit List/Running/reconcile for
   free.

### `Watch` stays per-backend

`Watch` is **not** unified. Docker keeps its push-based `die` event stream
(`docker.Events` filtered to this runner's containers); lambda keeps its
`ListMicrovms` poll loop. The reasons the two differ are intrinsic — Docker has
a real event bus, Lambda does not — and forcing a common abstraction would
either throw away Docker's push semantics or impose a poll on a backend that
doesn't need one. `Watch` *does* benefit indirectly: lambda's poll already
iterates `taskstate`, and Docker's `Watch` can stop parsing the task id out of
event attributes by looking the container id up in the store
(`store.byHandle`) — but the loop structure stays backend-specific.

The orchestrator (`runner.Runner`) is **untouched**: `Reconcile`, `Prune`, and
`Monitor` still call `backend.List` / `backend.Watch` and consume
`[]backend.Sandbox` / `backend.Exit` exactly as today. This change lives
entirely under the `backend` seam.

### Package layout

```
internal/runner/
├── taskstate/                  promoted, shared, atomic-write store
│   └── taskstate.go            Record{TaskID, Backend, Handle}; Write/Read/Remove/List
├── runner.go                   unchanged orchestrator
└── backend/
    ├── backend.go              Backend interface (see Open Questions)
    ├── docker/                 handle = {container_id}; probe = ContainerInspect
    └── lambdamicrovm/          handle = {microvm_id, ...}; probe = ListMicrovms lookup
```

### Migration

The lambda backend already writes `tasks/<id>.json`. Moving the package from
`internal/runner/backend/lambdamicrovm/taskstate` to `internal/runner/taskstate`
is a package move plus a record-shape change, so the on-disk format must be
handled deliberately:

- **Path is unchanged.** The store still owns `<state-dir>/tasks/<id>.json`.
  Lambda's state dir (`/var/lib/xagent/lambda-microvm/<runner-id>`) is unchanged;
  Docker gains a state dir for the first time.
- **Format changes** from the flat lambda `State` (`task_id`, `microvm_id`, …)
  to the generic `Record` (`task_id`, `backend`, `handle:{microvm_id, …}`).
  Since #1054 is **unmerged**, no lambda store exists in production yet, so the
  honest path is: land the generic shape with #1054 (or immediately after) and
  ship **no** flat-format files. If #1054 ships first and a flat file could
  exist, `Read` can fall back to decoding the legacy flat `State` when `backend`
  is empty and rewrite it as a `Record` on next `Write` — but the preferred
  outcome is to avoid the legacy format entirely by sequencing the merges.
- Docker has **no** prior store to migrate. Its first `List` after upgrade finds
  an empty store. Existing labelled containers from before the upgrade are
  therefore invisible to discovery — see the first trade-off; in practice the
  Docker container name `xagent-{taskID}` makes this self-healing on the next
  `Start`.

## Trade-offs

**Tags are informational, so a wiped state dir loses live sandboxes.** Because
nothing is ever discovered from labels/tags, a *fresh* runner with an empty or
deleted `tasks/` directory will **not** rediscover sandboxes that a prior
instance started on a shared daemon/account. This is the cost of decision #2 and
it is **accepted**: the state dir is persistent across same-host restarts (it
lives on the host, not in the sandbox), which covers the real recovery case (a
runner process restart re-reads its own `tasks/`). Cross-host failover or a
deliberately wiped state dir is **out of scope** — recovering that would require
re-opening tag-based discovery, which is exactly what this proposal removes. (An
optional boot-time reseed is listed as an Open Question.)

**Create-then-record orphan gap.** There is a window between creating a sandbox
and writing its record. A crash in that window orphans an untracked sandbox. The
two backends differ in how exposed they are:

- **Docker is self-protected.** The container name `xagent-{taskID}` is
  *deterministic*. A lost store-write after `ContainerCreate` means the next
  `Start` for that task tries to create `xagent-{taskID}` again and hits a
  **name conflict**, which *surfaces* the orphan instead of silently spawning a
  duplicate. (`Start` can resolve the conflict by adopting the existing
  container — inspect by name, re-record — turning the gap into a self-heal.)
- **Lambda is not self-protected.** The `microvmId` is **AWS-assigned** and only
  known *after* `RunMicrovm` returns, so the record is necessarily written
  *after* the VM exists. A crash between `RunMicrovm` returning and `state.Write`
  succeeding orphans a running, untracked microVM — and with tags no longer a
  discovery fallback (this is precisely #1054 Open Question #4), nothing will
  ever find it again. The only backstop is **`max_duration_seconds`** (≤ 8 h,
  surfaced as billing): the VM self-expires. We **accept** this and lean on
  `max_duration` as the reaper — the same backstop #1054 already relies on for a
  shim that fails to self-terminate. Operators who want a tighter bound set a
  lower `max_duration_seconds`. (Boot-time tag reseed, if adopted, would also
  close this; see Open Questions.)

**`RepairNetworks` is not removed by this change.** It would be tempting to read
this proposal as "stop using the daemon as truth ⇒ delete the messy Docker
reconcile code." But `RepairNetworks` exists because the Docker backend
**reuses** a task's container across restarts (to preserve its filesystem), and
a reused container's network endpoint id drifts after `docker compose down &&
up`. That complexity comes from container *reuse*, not from label-based
discovery, so it is orthogonal and stays exactly as is. This change removes
label *parsing*, not container reuse.

**One shared store directory vs. per-backend dirs.** A runner that advertises
multiple backends (#1054 allows one `workspaces.yaml` to serve heterogeneous
runners) could collide task ids across backends in one `tasks/` dir. The
`Record.Backend` field disambiguates and `List` filters to the active backend,
so a single directory is safe and keeps the path scheme uniform. The alternative
(a per-backend subdirectory) is also viable but buys little once records are
self-describing.

## Open Questions

1. **Boot-time tag reseed as an opt-in recovery path.** Should the shared store
   optionally **seed/repair itself from runtime tags at startup** — re-opening
   the discovery fallback *only at boot* — to cover the wiped-dir / cross-host
   case and the lambda orphan gap, while staying store-only during steady-state?
   This would be a single `reseed()` that lists by tag once on start and writes
   any missing records, then never consults tags again. It restores resilience
   at the cost of re-introducing the tag dependency the design just removed (and
   needs a tag→handle reconstruction per backend). Recommendation: ship
   **store-only** first; add reseed behind a flag if the orphan/failover cases
   prove painful in practice.

2. **Exact `Backend` interface impact.** Two shapes:

   - **(a) Shrink the interface to per-handle ops.** Replace `Start`/`Stop`/
     `Running`/`List`/`Remove` discovery with `Launch(spec) (handle, error)`,
     `Probe(handle) (State, error)` (or `ProbeAll([]handle) []State` for
     batching), `Signal(handle)`, `Destroy(handle)`, and let a **shared wrapper**
     in `package backend` own the store and implement `List`/`Running`/`Remove`/
     `Reconcile` on top. Future backends implement four small methods. This is
     the larger, cleaner refactor and the one that delivers the "new backends
     implement only launch/probe/signal/destroy" promise — but it churns the
     interface and every existing backend + its moq.
   - **(b) Keep the current `Backend` interface, share only a helper.** Backends
     keep `Start/Stop/Running/List/...`, but each delegates to a shared
     `taskstate.Store` and a shared `Reconcile(store, probe)` helper. Smaller
     diff, no interface churn, but each backend still writes a thin List/Running
     that calls the helper.

   **Recommendation: (a)**, sequenced after #1054 lands so there is a second
   real backend to validate the seams against (designing the per-handle
   interface from Docker alone risks baking in container-shaped assumptions). If
   the churn is judged too large for one PR, land (b) as an intermediate step —
   it is forward-compatible with (a).

3. **Migration / format compatibility.** Spelled out under *Migration* above.
   The open decision is whether to **sequence the merges** so the flat lambda
   `State` never reaches disk (preferred — #1054 is unmerged), or to ship a
   legacy-format `Read` fallback (decode flat `State` when `backend` is empty,
   rewrite as `Record` on next `Write`) to be safe if #1054 lands first. Which
   sequencing does the maintainer want?
