# Server-side taskstate: durable task→sandbox-handle mapping

Issue: https://github.com/icholy/xagent/issues/1160

Reconsiders decision #1 of
[`proposals/draft/shared-runner-taskstate.md`](shared-runner-taskstate.md)
("runner-owned, not the server"). That proposal introduced the shared
`internal/runner/taskstate` store and deliberately kept it on the runner's local
disk. This proposal keeps the *store abstraction* and the runner's role as sole
writer, but relocates the **backing store** from local JSON files to the server's
Postgres — making the mapping durable across a runner's disk, and a precondition
for any failover story.

## Problem

The runner is the single owner of the task→sandbox-handle mapping, and today that
mapping lives only on the runner's disk.

**What is stored.** `internal/runner/taskstate/taskstate.go` persists one record
per task:

```go
type Record struct {
    TaskID int64           `json:"task_id"`
    Type   string          `json:"type"`           // "docker", "lambda-microvm", ...
    ID     string          `json:"id"`             // backend-produced handle id
    Data   json.RawMessage `json:"data,omitempty"` // opaque; the store NEVER decodes it
}
```

One atomic `<state-dir>/<id>.json` file per task (marshal → temp file → `fsync` →
`os.Rename`), default `/var/lib/xagent/tasks`, configured by `--state-dir` /
`XAGENT_STATE_DIR` and opened in `internal/command/runner.go` via
`taskstate.Open(...)`. The record is the decomposed form of a `backend.Handle`
(`internal/runner/backend/backend.go`): `Type`/`ID`/`Data`. For the **Docker**
backend `ID` is the container id and `Data` is nil; for the **lambda-microvm**
backend `ID` is the MicroVM id and `Data` is `{endpoint, image_arn, stage_bucket,
stage_key}` — the endpoint the runner needs to reach the VM for SSE + `/xagent/stop`
and resume, plus the staged-object references `Destroy` cleans up.

**Lifecycle (every call site, `internal/runner/runner.go`).** The runner is the
only writer; backends never touch the store.

- **Write** — `Start` (`runner.go:487`), after `backend.Launch` returns a handle:
  `store.Write(Record{task.ID, h.Type, h.ID, h.Data})`.
- **Read** — `handle(taskID)` (`runner.go:309`) reconstructs a `backend.Handle`
  from the record; called by `Start` (to decide reuse vs. fresh), `Remove`, and
  `Kill`.
- **Remove** — `Remove` after `backend.Destroy` (`runner.go:362`), `gone` when a
  reuse handle turns out `ErrGone` (`runner.go:508`), and `Load` for a dangling
  record (`runner.go:276`).
- **List** — `Load` at startup (`runner.go:256`) enumerates every record to
  re-attach a supervise goroutine per running sandbox and seed the concurrency
  semaphore; `List` (`runner.go:333`) enumerates to compose the orchestrator's
  sandbox view, which `Prune` walks to destroy sandboxes of archived/deleted tasks.

**Where the handle is needed.** `RestartTask` and `OpenShell`
(`internal/server/apiserver/shell.go`) both relaunch a *finished* task's sandbox
against its preserved disk: the server issues a start, the runner reads the stored
handle, and `backend.Launch(spec, reuse)` adopts the exact recorded sandbox
(`docker.ensure` inspects the container by id; `lambdamicrovm.tryResume` resumes
the MicroVM by id). Without the handle there is nothing to adopt — a restart would
either fail or, worse, spawn a duplicate.

**Why local disk is a problem.** The mapping is bound to one runner's volume:

1. **Runner replaced with a fresh volume loses everything.** A runner rescheduled
   onto a new disk (a new host, a recreated container, a wiped `--state-dir`) has
   an empty store. It can no longer relaunch, reconcile, or clean up any sandbox a
   prior instance created. `shared-runner-taskstate.md` names this exact case an
   **accepted** trade-off ("a wiped state dir loses live sandboxes") and scopes
   cross-host failover out.
2. **No failover / multi-runner story.** A *different* runner cannot learn which
   sandbox belongs to a task, even when the handle is portable. A `lambda-microvm`
   handle (MicroVM id + endpoint) is meaningful to **any** runner with AWS creds in
   that account/region — but the only copy sits on the original runner's disk.
3. **Orphan leaks.** If the originating runner's disk does not survive, a running
   MicroVM (billed until its `max_duration` backstop) is leaked with nothing left
   that knows it exists. The docker case is self-healing via the deterministic
   `xagent-{taskID}` container name; the cloud case is not.

The server already coordinates the runner on every poll and event, already owns
the task's logical state in Postgres (`internal/store/`, `tasks` table), and
already records which runner a task is assigned to (the `runner` column). Storing
the sandbox handle there — **opaquely**, exactly as `taskstate` already treats
`Data` — makes the mapping durable and decoupled from any one runner's disk.

## Design

### Overview

Keep `taskstate` as the runner's abstraction and keep the runner the **sole
writer**, but swap the backing store from local JSON files to the server over RPC.
The record's shape is unchanged (`{TaskID, Type, ID, Data}`); it is persisted in a
new server-side table and read back through the task poll the runner already does.
The server treats the handle as an **opaque blob** — it never decodes `Type`,
`ID`, or `Data`, exactly as the local store never decodes `Data` today.

Four decisions frame the design.

1. **The handle is opaque to the server; the runner is still the only interpreter.**
   The core objection in `shared-runner-taskstate.md` decision #1 is that "a handle
   is meaningful only to the runner that created it (a container id is local to one
   daemon; a microVM id to one account/region), and persisting it server-side would
   couple the server to backend internals it has no reason to know." This design
   **agrees with the premise and rejects the conclusion.** The server stores
   `{backend, handle_id, handle_data}` and never interprets any of it — no decode,
   no dispatch, no lifecycle logic. All meaning stays with the runner, which is the
   only component that translates a stored record back into a `backend.Handle` and
   calls `Probe`/`Launch`/`Destroy`. Storing an opaque value is not the same as
   understanding it; the server already stores plenty it does not interpret (link
   URLs, event payloads). *Host-locality* is real and is preserved — see decision 3
   — but it is a constraint on *who can act on* a handle, not a reason the mapping
   must be non-durable.

2. **A dedicated `sandboxes` table, not columns on `tasks`.** The mapping is
   1:1 with a task, which argues for columns on the `tasks` row. But a dedicated
   table is chosen because: the handle exists only while a sandbox does (most
   terminal/archived tasks have none, so a nullable side table is more honest than
   three usually-null columns on the hot task row); it keeps the frequently-read
   `tasks` row small; and it gives a natural place to index by `runner` for
   reconciliation and to carry a per-handle `updated_at`. The `tasks` row is
   already contended on every command/status write; the handle is written on a
   different cadence (sandbox create/teardown).

   ```sql
   -- internal/store/sql/migrations/NNNN_sandboxes.sql
   CREATE TABLE public.sandboxes (
       task_id     bigint      NOT NULL REFERENCES public.tasks(id) ON DELETE CASCADE,
       org_id      bigint      NOT NULL,
       runner      text        NOT NULL,   -- runner that produced the handle
       backend     text        NOT NULL,   -- == Record.Type; informational
       handle_id   text        NOT NULL,   -- == Record.ID; opaque
       handle_data jsonb,                  -- == Record.Data; opaque, server never decodes
       updated_at  timestamp   NOT NULL DEFAULT CURRENT_TIMESTAMP,
       PRIMARY KEY (task_id)
   );
   CREATE INDEX idx_sandboxes_runner ON public.sandboxes (runner, org_id);
   ```

   `ON DELETE CASCADE` means deleting a task drops its handle for free. `org_id` is
   carried for scope enforcement (every server query is org-scoped, matching
   `GetTask`/`ListTasksForRunner`). The `runner` column records **which runner's
   world the handle belongs to** — decisive for host-local backends (decision 3).

3. **Runner-scoped actionability; the `runner` column is the guard.** A Docker
   container id is only meaningful on the daemon that created it; a `lambda-microvm`
   id is meaningful to any runner in the same account/region. The server does not
   need to know which is which — it stores the producing `runner` alongside the
   handle and hands it back. The **runner** decides:

   - A runner only ever fetches handles for tasks assigned to it (`task.runner ==
     its own id`), because it only polls `ListRunnerTasks(runner=self)`. So the
     steady state already matches producer to consumer.
   - When a runner adopts a handle whose stored `runner` differs from its own id
     (a reassigned task, a renamed runner), it must treat a host-local handle as
     unusable. For Docker this is automatic: `docker.ensure` inspects the container
     by id, the id does not exist on the new daemon, `Launch` returns `ErrGone`, and
     the runner falls back to its self-healing path (deterministic
     `xagent-{taskID}` name). For `lambda-microvm` the handle *is* portable, so a
     replacement runner in the same region can `tryResume` it — this is the failover
     win. The distinction lives in the backend's `Launch`/`Probe`, unchanged; the
     server stays ignorant of it.

   Concurrency between two runners is bounded by the existing assignment model: a
   task has exactly one `runner`, and `ListRunnerTasks` filters by it, so two
   runners do not race for the same task's sandbox unless an operator repoints
   `task.runner` mid-flight. Writes are guarded by an optimistic check (below) so a
   stale writer cannot clobber a newer handle.

4. **The runner stays sole writer; reads ride the existing poll.** No backend and
   no other server path writes `sandboxes`. The runner writes it at the same
   two moments it writes the local store today (after `Launch`, after `Destroy`),
   and reads it at the same moments (`Start`, `Load`, `List`). Reads for the hot
   path (`Start`) piggyback on `ListRunnerTasks`, which already returns the tasks
   the runner is about to act on — the handle travels with the task, so `Start`
   needs no extra round trip.

### API changes

A new opaque message and three RPCs, plus one field on `Task`.
(`proto/xagent/v1/xagent.proto`.)

```proto
// Opaque, backend-defined sandbox handle. The server stores and returns it
// verbatim and never interprets any field. Mirrors taskstate.Record.
message Sandbox {
  int64  task_id     = 1;
  string runner      = 2;  // runner that produced the handle
  string backend     = 3;  // == taskstate.Record.Type; informational
  string handle_id   = 4;  // == taskstate.Record.ID
  bytes  handle_data = 5;  // == taskstate.Record.Data (opaque JSON); may be empty
}

// Runner → server: persist the handle after backend.Launch. Idempotent upsert.
message SetTaskSandboxRequest {
  Sandbox sandbox = 1;
  // Optimistic guard: the task version the runner acted on. The server rejects
  // the write if the task advanced past it (a concurrent reassignment/restart),
  // mirroring the version guard SubmitRunnerEvents already uses.
  int64 version = 2;
}
message SetTaskSandboxResponse {}

// Runner → server: drop the handle after backend.Destroy. Idempotent.
message ClearTaskSandboxRequest { int64 task_id = 1; }
message ClearTaskSandboxResponse {}

// Runner → server: enumerate the handles this runner owns, for Load/Prune.
// Replaces the local store.List() scan.
message ListRunnerSandboxesRequest { string runner = 1; }
message ListRunnerSandboxesResponse { repeated Sandbox sandboxes = 1; }
```

```proto
service XAgentService {
  // ...
  rpc SetTaskSandbox(SetTaskSandboxRequest) returns (SetTaskSandboxResponse);
  rpc ClearTaskSandbox(ClearTaskSandboxRequest) returns (ClearTaskSandboxResponse);
  rpc ListRunnerSandboxes(ListRunnerSandboxesRequest) returns (ListRunnerSandboxesResponse);
}
```

And the handle rides the poll so `Start` needs no extra fetch:

```proto
message Task {
  // ... existing fields ...
  // Sandbox handle for this task, if one is recorded. Populated by
  // ListRunnerTasks/GetTask so the runner resolves reuse without a second call.
  Sandbox sandbox = 17;
}
```

`ListRunnerTasks` (the runner's poll) left-joins `sandboxes` and fills
`Task.sandbox` when present. Because the runner already receives the task on every
poll, the read path for `Start` is free; the standalone `ListRunnerSandboxes`
exists only for the enumerate-everything paths (`Load` at boot, `Prune`).

### Server handlers and auth

New handlers in `internal/server/apiserver/` (alongside `SubmitRunnerEvents` in
`runner.go`), each `apiauth.MustCaller(ctx)` + org-scoped, gated on
`authscope.OpTaskWrite` for the mutating RPCs and `OpTaskRead` for the list — the
same scopes `SubmitRunnerEvents` and `ListRunnerTasks` already require. New store
methods in `internal/store/task.go` (sqlc queries in
`internal/store/sql/queries/`): `UpsertTaskSandbox`, `DeleteTaskSandbox`,
`GetTaskSandbox`, `ListTaskSandboxesForRunner`.

**Auth honesty — there is no runner principal today.** The runner authenticates
with an **org-scoped `xat_` API key**, not a per-runner identity; `task.runner` is
unauthenticated *data* that any org member with `task.write` can set (see
`ListRunnerTasks`, whose `req.Runner` is untrusted input). So the server **cannot**
cryptographically verify that a `SetTaskSandbox` caller *is* the runner it claims.
The enforceable guarantees are the same ones the runner event path already relies
on: (a) org scoping — a caller can only write handles for tasks in its own org; (b)
task-write scope; (c) the optimistic `version` guard against a stale writer; and
(d) the server records the caller-supplied `runner` string so a consumer can detect
"this handle came from a different runner's world" (decision 3). Strengthening this
to a real runner principal is the same gap
[`proposals/implemented/eliminate-runner-socket-proxy.md`](../implemented/eliminate-runner-socket-proxy.md)
navigated for drivers (server-verifiable app JWTs); a runner-scoped credential is
**out of scope here** and listed as an open question. This design does not make the
trust model worse than the local store — a local store on a shared host is equally
unauthenticated — it just moves the same trust boundary to the server.

### Runner changes

`internal/runner/taskstate` keeps its `Store` shape but gains a server-backed
implementation. The cleanest form is an interface the runner depends on, with two
implementations (local files today, server RPC after the cutover):

```go
// same method set the runner already calls
type Store interface {
    Write(rec Record) error
    Read(taskID int64) (Record, bool, error)
    Remove(taskID int64) error
    List() ([]Record, error)
}
```

- A `serverstore` implementation maps `Write`→`SetTaskSandbox`,
  `Remove`→`ClearTaskSandbox`, `List`→`ListRunnerSandboxes`, and `Read`→the handle
  already delivered on the polled `Task` (falling back to `GetTask` for the rare
  out-of-band read). Translation between `Record` and `backend.Handle` stays in
  `runner.go` exactly as it is — nothing in the orchestration logic
  (`Start`/`Load`/`List`/`Remove`/`Prune`) changes shape; only where the bytes land
  changes.
- `internal/command/runner.go` selects the implementation. `--state-dir` remains,
  but for a server-backed runner it is either unused or a local **cache** (below).

Because `Write`/`Remove`/`List` become network calls, they inherit the runner's
existing resilience: the retrying `EventQueue` already used for `SubmitRunnerEvents`
buffers a `SetTaskSandbox`/`ClearTaskSandbox` that races a server blip, so a
transient outage does not lose a handle (see Failure modes).

### Rollout

There is a single operator and a single runner today, so there is no mixed fleet to
stay compatible with and no staged dual-write is needed — the change is a direct
cutover:

1. **Land the schema + RPCs.** `sandboxes` and the three RPCs ship together. The
   table is written immediately; it does not need to be dormant-then-backfilled the
   way a multi-runner rollout would.
2. **Switch the runner to the server store.** `internal/command/runner.go`
   constructs the `serverstore` implementation instead of the file store. A one-time
   boot reconcile (`for rec in localStore.List(): SetTaskSandbox(rec)`) seeds any
   handles the local disk still holds so no currently-running sandbox is orphaned by
   the switch; after that the local files are unused.
3. **Keep or drop the local store.** The file implementation stays in the tree as an
   optional write-through cache (Open Question 2) or is removed outright — nothing
   else depends on it, and either choice is safe.

Because the server is the sole store from step 2 on, a runner rescheduled onto a
fresh volume rehydrates entirely from the server — the durability win. If a broader
fleet ever appears, the new table and the additive `Task.sandbox` field mean older
runners simply ignore them, so a staged dual-write could be layered on then; that is
explicitly not built now.

### Failure modes

- **Server unreachable during sandbox create.** `backend.Launch` succeeded but
  `SetTaskSandbox` cannot reach the server. The handle write is enqueued on the
  retrying `EventQueue` (as `SubmitRunnerEvents` already is) and retried; during
  the gap the runner holds the handle in memory / the local cache, so its own
  supervise goroutine is unaffected. If the runner *also* crashes in that window,
  the sandbox is orphaned exactly as today — but Docker self-heals on the next
  `Start` via the deterministic `xagent-{taskID}` name, and `lambda-microvm` leans
  on `max_duration` as the reaper (the same backstop
  `lambda-microvm-backend.md`/`shared-runner-taskstate.md` already document). Moving
  the store to the server does **not** widen this window; it narrows the *recovery*
  gap, because a replacement runner can now see every handle that *did* commit.
- **Server unreachable during teardown.** `backend.Destroy` ran but
  `ClearTaskSandbox` did not — the server holds a stale handle. Self-healed on the
  next reconcile: `Load`/`Prune` fetch via `ListRunnerSandboxes`, `Probe` the
  handle, get `StateGone`, and clear it — identical to how `Load` drops a dangling
  local record today (`runner.go:276`), just against server state.
- **Stale/orphaned handle from a runner that never returns.** The server holds a
  Docker handle for a dead host. Because the id is host-local, no replacement runner
  can act on it — but the record is harmless (it is cleared when the task is
  archived, via `ON DELETE CASCADE` on delete, or when a runner reclaims the task
  under the same id and `Probe` reports `StateGone`). A server-side sweep (clear
  handles for archived tasks) can be added if leaks prove noisy; for portable
  `lambda-microvm` handles a same-region runner reclaims and reaps them.
- **Teardown still happens after a disk loss.** This is the payoff: a runner that
  lost its local disk enumerates its sandboxes from the server
  (`ListRunnerSandboxes`) and can `Destroy` them — the exact case the local store
  cannot recover and that `shared-runner-taskstate.md` scoped out.

### Package layout

```
internal/runner/taskstate/
├── taskstate.go        Record{...}; Store interface (unchanged method set)
├── filestore.go        existing atomic-JSON impl (renamed; kept as cache/fallback)
└── serverstore.go      new: Store backed by Set/Clear/ListRunnerSandboxes RPCs

internal/store/
├── task.go             + Upsert/Delete/Get/ListTaskSandboxesForRunner
└── sql/
    ├── migrations/NNNN_sandboxes.sql
    └── queries/sandbox.sql

internal/server/apiserver/runner.go   + SetTaskSandbox/ClearTaskSandbox/ListRunnerSandboxes
proto/xagent/v1/xagent.proto          + Sandbox, 3 RPCs, Task.sandbox
```

Nothing in `internal/runner/backend/` changes: backends still return a
`backend.Handle`, and the runner still translates `Handle ↔ Record` at the
boundary. Only the `Store` implementation behind that boundary moves.

## Trade-offs

**Server round-trips on the sandbox lifecycle path.** `Write`/`Remove` become
network calls instead of `fsync`+`rename`. This is acceptable: sandbox
create/teardown is already dominated by container/VM operations (seconds), a
handle write is tiny, and the hot read path (`Start`) piggybacks on a poll the
runner already makes — no added round trip. The `EventQueue` already absorbs
transient server failures for the comparable `SubmitRunnerEvents` path.

**Reintroduces a dependency the local store removed.** `shared-runner-taskstate.md`
valued a runner that can track its sandboxes with the server unreachable. Server-backed
taskstate couples handle persistence to server availability. Mitigated by keeping a
local write-through **cache** (Open Question 2) so an already-launched sandbox is
still supervised during a server blip; the cache is best-effort and the server is
authoritative. The net is a deliberate trade: slightly more coupling to the server
(which the runner already cannot function without for polling and tokens) in
exchange for durability and failover.

**The server stores something it cannot validate.** As covered under auth, the
server persists a runner-supplied opaque handle without a runner principal to
attribute it to. This is not a *regression* — the local store is equally
unauthenticated on a shared host — but centralizing it makes the missing runner
identity more visible. Treated as an open question rather than a blocker.

**Chose a side table over columns on `tasks`.** A 1:1 mapping tempts three columns
on the task row. The dedicated `sandboxes` table costs a join on the runner
poll but keeps the hot task row small, models "usually absent" honestly, and gives
a clean `runner` index for reconciliation. The join is cheap (primary-key lookup)
and only on the runner's own poll.

## Open Questions

1. **Runner principal / credential.** Should a runner authenticate as a
   *runner* (a runner-scoped credential the server can verify), so `SetTaskSandbox`
   and `ListRunnerSandboxes` can be attributed and a runner cannot write another
   runner's handles? This is the same server-verifiable-identity direction
   `eliminate-runner-socket-proxy.md` took for drivers. Recommendation: ship on the
   existing org-scoped key + `version` guard first (no worse than today), and treat
   runner identity as a separate, broader piece of work — it affects
   `ListRunnerTasks`, `RegisterWorkspaces`, and `SubmitRunnerEvents` too, not just
   this table.
2. **Keep a local cache, or go server-only?** The rollout leaves this open. A
   thin local write-through cache lets a same-host Docker restart re-attach without
   a server round trip and survives a brief server outage; server-only is simpler
   and is the only correct choice for a runner that expects to be rescheduled onto
   fresh disk. Recommendation: server-authoritative with an **optional** local
   cache, defaulting to on for the Docker backend and off (or tmpfs) for cloud
   backends.
3. **Server-side orphan GC for host-local handles.** A dead Docker runner leaves a
   handle no one can act on. Is archive-time / delete-time cleanup (`ON DELETE
   CASCADE` + a clear-on-archive) sufficient, or is a periodic server sweep that
   drops handles for long-dead runners worth building? Recommendation: rely on
   archive/delete cleanup first; add a sweep only if leaks are observed.
4. **`Read` fallback vs. always-embed.** Embedding `Task.sandbox` on every
   `ListRunnerTasks`/`GetTask` covers the hot path, but a few out-of-band reads
   (e.g. a targeted reconcile) would still hit `GetTask`. Is a dedicated
   `GetTaskSandbox` RPC worth adding, or is reusing `GetTask` (which now carries the
   handle) enough? Leaning to reuse `GetTask` and avoid RPC surface.
5. **Coordination with portable-handle backends.** For `lambda-microvm`, a durable
   server-side handle makes true cross-runner failover *possible* (a replacement
   runner in the same region resumes the VM). Should this proposal define the
   failover reassignment flow (who repoints `task.runner`, and when), or is that a
   follow-up that merely *consumes* the durable handle this proposal provides?
   Recommendation: this proposal delivers the durable mapping; the reassignment
   policy is a follow-up.
