# Send Driver Logs to the Server

Issue: https://github.com/icholy/xagent/issues/1241

## Problem

The driver (`xagent driver`) runs inside the sandbox and writes all of its
diagnostics to the container's stdio, where nothing collects them:

- The driver's own structured `slog` output — setup progress, loaded config,
  tool-call summary lines parsed from the Claude CLI stream
  (`internal/agent/claude.go`), and errors — goes to the default handler on
  stderr (`internal/command/driver.go` wires `slog.Default()`).
- Setup command stdout/stderr go straight to `os.Stdout`/`os.Stderr`
  (`Driver.setup` in `internal/agent/driver.go`).
- The Claude Code CLI's stderr is passed through to the container's stderr
  (`cmd.Stderr = os.Stderr` in `claude.go`).

The only in-container output that reaches the server today is the agent's
deliberate `report` tool calls (which become `report` events) and the one-line
`reason` on a `failed` runner event (rendered as "Sandbox failed: …").
Everything else requires shell access to the runner host to run
`docker logs xagent-{id}` — and even that dies with the sandbox:
`Runner.Prune()` removes containers for archived tasks, and restarts replace
them. Setup failures are the worst case: the timeline shows
`setup command 2 failed: exit status 1` with none of the command's output.

Driver logs should be shipped to the server, persisted, and viewable in the
Web UI.

## Current State

- **The `logs` table is gone.** `20260614000003_drop_logs.sql` dropped it as
  part of the unified-task-event-stream work: `llm` rows became `report`
  events, `audit`/`info`/`error` rows became `lifecycle` events, `mcp` rows
  were dropped. The `events` table is a *semantic* timeline — five typed
  payloads (`instruction`, `external`, `report`, `lifecycle`, `link`), each a
  meaningful moment in the task's life. That proposal explicitly deferred a
  verbose channel ("No separate verbose channel").
- **`UploadLogs` survives as a shim.** The RPC still exists
  (`internal/server/apiserver/log.go`) but only re-points the agent's `report`
  tool (`type='llm'`) onto the event stream; every other entry type is
  silently dropped.
- **The driver talks directly to the server.** Since
  `eliminate-runner-socket-proxy`, the runner mints a task-scoped app JWT via
  `CreateTaskToken` and the driver dials the real server URL over HTTPS with
  it. Handlers enforce per-task access with
  `caller.Scopes.Allow(op, task.ScopeAttr()...)`. There is no socket proxy to
  extend; any new RPC rides the same authenticated client
  (`internal/xagentclient`).
- **The driver owns lifecycle reporting.** Per `driver-owned-events`, the
  driver emits `started`/`stopped`/`failed` via `SubmitRunnerEvents` and its
  exit code means "did I report?". The terminal submit runs on a context that
  survives SIGTERM (`Driver.Run`), which gives a natural final-flush point.
- **Run identity is being formalized.** The `run-scoped-runner-events` draft
  makes `Task.Version` the run identity: the version bumps exactly when a new
  run is provisioned, and every runner event carries it. Driver logs should
  adopt the same identity so a task's logs group cleanly by run.
- **The server's disk is ephemeral on Fly.** `fly.toml` mounts no volume, and
  the deployment is pinned to a single machine (the in-process pubsub
  comment). Filesystem storage on the server therefore needs a Fly volume;
  self-hosted deployments just need a directory.

## Design

The driver ships its combined diagnostic output as **one raw byte stream per
run** — the same interleaved text `docker logs` shows today — and the server
appends it to a flat file. There is no line framing, no levels, no structured
fields on the wire: the stream is already human-readable text (slog's default
handler prints `time=… level=… msg=…`), and a byte offset gives ordering,
resumption, and idempotency for free, because the size of the server-side
file *is* the protocol state.

### What goes into the stream

Everything the driver's process tree writes to stdio, captured with tees (the
container's own stdio is unchanged, so `docker logs` still works and a broken
shipper never blinds local debugging):

- The driver's `slog` output: `command/driver.go` wraps the default handler's
  output in a tee so rendered records also feed the shipper.
- Setup command output: `Driver.setup` sets `c.Stdout`/`c.Stderr` to
  `io.MultiWriter(os.Stdout, shipper)` (and stderr respectively).
- The agent CLI's stderr: `claude.go` (and the other providers) set
  `cmd.Stderr = io.MultiWriter(os.Stderr, shipper)`.

Deliberately **out of scope**:

- The Claude CLI's stdout `stream-json` transcript. It is enormous, embeds
  full file contents, and the driver already distills it into `tool
  name=… summary=…` slog lines (`richer-tool-call-logs`), which ship as part
  of the stream. The raw transcript stays in the container.
- The agent's `report` tool output. Reports are agent-authored timeline
  content and stay `report` events. Driver logs are machine diagnostics; the
  two never mix.

### Wire: offset-based appends

```proto
message SubmitDriverLogsRequest {
  int64 task_id = 1;
  int64 version = 2;  // run identity; 0 until run-scoped-runner-events lands
  int64 offset = 3;   // byte offset from the start of this run's log
  bytes data = 4;     // max 64 KiB per request
}

message SubmitDriverLogsResponse {
  // Authoritative size of the run's log after the append — the offset the
  // driver must use next. Normally offset + len(data); smaller if the server
  // truncated for the cap, different if the two sides had diverged.
  int64 size = 1;
  // True once the per-run byte cap is reached; the driver stops shipping
  // (but keeps writing to stdio).
  bool capped = 2;
}
```

The append rule is plain file semantics — the server only ever writes at the
end of the file, and the file size resolves every race:

- `offset == size`: append, return the new size.
- `offset < size`: a retry of bytes the server already has (the previous
  response was lost). The server skips the overlapping prefix, appends any
  remainder, and returns the size. Redelivery is invisible.
- `offset > size`: the two sides diverged — only possible if the server lost
  the file tail (redeploy without a volume). The server appends nothing and
  returns its current size; the driver rebases: it writes a
  `\n[xagent] gap: N bytes lost\n` marker into its own stream and resumes
  sending from the returned size. Converges in one round trip.

There is no separate idempotency state to persist or recover: after a server
restart the file size is right there in the filesystem.

Server-side enforcement, independent of driver behavior: ≤ 64 KiB per
request, and a per-run cap (default 16 MiB). At the cap the server appends a
final `\n[xagent] log cap reached\n` marker and returns `capped=true`.

**Auth** follows the `SubmitRunnerEvents` / `UploadLogs` handler shape:
coarse `AllowOp(OpTaskWrite)` gate, load the task, instance check
`Allow(OpTaskWrite, task.ScopeAttr()...)`. The task-scoped JWT from
`CreateTaskToken` already satisfies this for the driver's own task and
nothing else; archiving the task revokes it via the `task.archived`
predicate. No new scope ops.

The handler publishes a `change` notification with a
`{Action: "appended", Type: "driver_logs", ID: task_id}` resource — the same
pattern `UploadLogs` uses with `task_logs` — so the Web UI's existing SSE
wake-and-refetch loop picks it up; the flush cadence below throttles this to
at most one notification per flush.

### Wire: offset-based reads

```proto
message ReadDriverLogsRequest {
  int64 task_id = 1;
  int64 version = 2;  // which run; 0 = the latest run
  int64 offset = 3;   // byte offset to read from
  int64 limit = 4;    // max bytes to return (default 256 KiB, max 1 MiB)
}

message DriverLogRun {
  int64 version = 1;
  int64 size = 2;
}

message ReadDriverLogsResponse {
  bytes data = 1;
  int64 next_offset = 2;             // offset + len(data)
  int64 size = 3;                    // current total size of this run's log
  repeated DriverLogRun runs = 4;    // all runs for the task, for the run picker
}
```

`ReadDriverLogs` is guarded by `OpTaskRead` + `task.ScopeAttr()`. One RPC
serves the whole UI: the `runs` list drives a run picker, `size` tells a
follower whether there is more to fetch, and tailing is
`offset = max(0, size - N)`. No streaming RPC — the UI convention is SSE
signal + refetch, and offsets make refetch cheap.

### Storage: flat files behind a `logstore` interface — not Postgres

Log bytes are bulk diagnostics, not relational state: nobody joins on them,
they are read back rarely, and retention is "delete the run". A Postgres
table would bloat the primary OLTP database and its backups and turn
retention into DELETE/vacuum churn. Postgres keeps holding the semantic
state (tasks, events, links); log bytes go to append-only files behind a
small interface, following the stdlib-only file-store pattern the runner
already trusts for `taskstate` and the outbox `FileStore`:

```go
// internal/server/logstore
type Store interface {
    // Append writes data at offset per the rules above and returns the
    // resulting size. Appends at the cap truncate and report capped=true.
    Append(ctx context.Context, taskID, version, offset int64, data []byte) (size int64, capped bool, err error)
    // Read returns up to limit bytes at offset, the run's current size,
    // and the task's runs.
    Read(ctx context.Context, taskID, version, offset, limit int64) (data []byte, size int64, runs []Run, err error)
    // Delete removes all logs for the task.
    Delete(ctx context.Context, taskID int64) error
}
```

- Layout: `<log-dir>/driver-logs/<task-id>/<version>.log` — one plain text
  file per run, exactly the bytes the driver sent. `grep`, `tail`, and `less`
  work on the server host with no tooling.
- Appends are buffered writes; no fsync — these are best-effort diagnostics
  and the crash window is one batch.
- **Server flag**: `--log-dir` (default under the server's working
  directory). The Fly deployment adds a `[mounts]` volume for it; without a
  volume the logs don't survive a redeploy — degraded, not broken (the
  offset rebase above resynchronizes the driver automatically).
- Access control never touches the filesystem: handlers load the task row
  from Postgres first and enforce scopes on it; the store is keyed by the
  already-authorized task id.
- An object-storage backend (S3/Tigris) is a possible later drop-in behind
  the same interface for deployments that outgrow a disk; deliberately not
  designed in now.

### Driver-side capture and shipping

A new `internal/agent/logship` package owns buffering and delivery:

```go
type Shipper struct { /* … */ }

func New(opts Options) *Shipper            // Client, TaskID, Version, Log
func (s *Shipper) Write(p []byte) (int, error) // io.Writer; never blocks, never errors
func (s *Shipper) Run(ctx context.Context)     // flush loop, exits when ctx done
func (s *Shipper) Flush(ctx context.Context) error // final drain, bounded by ctx
```

- **One writer, byte-oriented**: all tees feed the same `io.Writer`;
  interleaving granularity is whatever the producers write, same as the
  container's stdio today. No line splitting, no per-line limits.
- **Batching**: writes go into an in-memory ring buffer (cap 1 MiB). A single
  flush goroutine sends one in-flight `SubmitDriverLogs` at a time — up to
  64 KiB per request, whenever bytes are pending and at most every 2 s — so
  ordering needs nothing beyond the offset.
- **Backpressure**: `Write` never blocks on the network. If the buffer is
  full (server slow or unreachable), the oldest bytes are dropped and a
  `\n[xagent] dropped N bytes\n` marker takes their place in the stream, so
  gaps are visible in the file itself and offsets stay contiguous.
- **Retry**: transient RPC errors back off and retry from the last
  acknowledged offset (overlap is skipped server-side). Permanent errors
  (`PermissionDenied` — e.g. task archived, `NotFound`, `InvalidArgument`,
  `Unimplemented`) or `capped=true` stop the shipper for the rest of the
  run; stdio output continues untouched. This mirrors the `isPermanentError`
  classification in `internal/runner/eventoutbox.go`.
- **Final flush**: `Driver.Run` already keeps `eventCtx` alive through
  SIGTERM for the terminal `SubmitRunnerEvents`. Before that terminal submit
  it calls `shipper.Flush(eventCtx)` with a short bound (~5 s). The flush is
  best-effort: a failure is logged and does **not** affect the exit code —
  the "did I report?" bit stays exclusively about the lifecycle event.
- **Run identity**: the driver fills `version` from the `GetTask` response it
  already fetches in `Driver.run` (bytes buffered before that are sent once
  the version is known). Once `run-scoped-runner-events` lands and the runner
  passes the provisioned version explicitly, the shipper uses that instead —
  same value, delivered earlier and immune to mid-run command bumps.

### No durable outbox in the container

The runner's lifecycle events go through a durable filesystem outbox because
the runner host outlives the events. The driver's filesystem **is** the
sandbox: any spooled log files die with the container, exactly when the logs
would be needed. A durable outbox buys nothing here — logs are best-effort
in-memory, and correctness-critical reporting stays on `SubmitRunnerEvents`.

### Web UI

- The task page (`webui/src/routes/tasks.$id.tsx`) gains a **Logs** tab
  beside the timeline: a monospace terminal-style pane rendering the raw
  text, a run picker fed by `runs`, and a follow toggle. Any level coloring
  is cosmetic client-side highlighting of the text (slog's `level=ERROR` is
  right there in the line); the server attaches no semantics.
- Data comes from `ReadDriverLogs` via connect-query, refetched from the last
  `next_offset` when the SSE `change` notification carries a `driver_logs`
  resource for the task — a follow session only transfers new bytes.
- The timeline is unchanged. `report` events remain the agent's authored
  output in the conversation view; driver logs are a separate diagnostic
  surface, visually distinct (terminal styling vs. timeline cards), so the
  two cannot be confused.

### Retention

- Deleting is `rm -r` of the task's directory — no DELETE churn, no vacuum.
- Archived tasks keep their logs for a grace window: the existing archiver
  loop (`internal/server/archiver`) gains a step that calls
  `logstore.Delete` for tasks archived more than 7 days ago (batch-limited
  per tick, like `Tick()`'s task batching). Live and recently-archived tasks
  stay fully debuggable; the directory cannot grow without bound.
- There is no FK cascade with file storage, so the same sweep removes
  directories whose task id no longer exists in Postgres (orphans from
  org/task deletion).
- The per-run cap (16 MiB) bounds a runaway run; in practice runs emit a few
  hundred KiB.

## Failure Modes

- **Driver crash / OOM / SIGKILL**: bytes since the last flush (≤ 2 s of
  output, plus anything the network delayed) are lost. The runner's
  `supervise` backstop still emits `failed`. The shipped prefix usually
  contains the interesting part; a future runner-side backstop could
  `docker logs --tail` on abnormal exit and submit through the same RPC (the
  request shape is producer-agnostic), but that is out of scope here.
- **Server unreachable / socket errors**: the ring buffer absorbs up to
  1 MiB, then drops oldest with a visible `dropped N bytes` marker. The
  agent is never blocked or failed because of log delivery.
- **Server disk full / write error**: `Append` fails, the handler returns
  `Internal`, the driver backs off and retries, eventually dropping with
  markers. Log delivery degrades; tasks and events are unaffected because
  they live in Postgres.
- **Server restart or redeploy**: no state to recover — the file size is the
  protocol state. If the tail (or whole file) was lost to an ephemeral disk,
  the next append's `offset > size` mismatch returns the authoritative size
  and the driver rebases with a gap marker; the pipeline resumes in one
  round trip. On Fly the log dir should be a mounted volume to avoid that
  loss entirely. (The deployment is a single machine by design — see the
  pubsub note in `fly.toml` — so no cross-instance coordination is needed.)
- **Task archived mid-run**: the token's `task.archived:"false"` predicate
  turns submits into `PermissionDenied`; the shipper classifies it permanent
  and goes quiet, matching how the sandbox is about to be pruned anyway.
- **Retry duplicates**: `offset < size` overlap is skipped by the append
  rule; redelivery is invisible.
- **Restart while old run still flushing**: the old driver's bytes carry the
  old `version` and land in the old run's file, the new run's in the new one
  — no interleaving, no clobbering. (With `version=0` before
  run-scoped-runner-events lands, both runs share a file and the second
  run's `offset` mismatch resolves via the gap-marker rebase; acceptable
  during the interim, see Open Questions.)
- **Log flood**: the request size limit, buffer cap, and per-run cap fail in
  that order; every drop point leaves a marker in the stream. The failure
  mode is "logs get sparse", never "task fails" or "server tips over".

## Implementation Plan

1. **`logstore` package** — Delivers: the `Store` interface and the
   filesystem implementation (per-run files, offset append rules, cap,
   reads, delete). Depends on: nothing. Verifiable by: unit tests
   (append/overlap/divergence/cap/read/delete) against a temp dir.
2. **Proto + generated code** — Delivers: `SubmitDriverLogs` /
   `ReadDriverLogs` messages and RPCs, `mise run generate` output, moq
   regeneration. Depends on: nothing (mergeable before 1; handlers don't
   exist yet). Verifiable by: build + generated-code diff review.
3. **Server handlers** — Delivers: the two handlers in
   `internal/server/apiserver` with scope checks, size limits, the
   `driver_logs` change notification, and the `--log-dir` flag wiring.
   Depends on: (1), (2). Verifiable by: handler tests following
   `log_test.go` / `runner_test.go` patterns (permissions, offset rules,
   cap, request size limit).
4. **Driver shipper** — Delivers: `internal/agent/logship` (ring buffer,
   flush loop, offset tracking, rebase) plus the tee wiring in
   `command/driver.go`, `Driver.setup`, and the agent providers' stderr;
   final flush in `Driver.Run`. Depends on: (2), (3). Verifiable by: unit
   tests with the moq client (ordering, drop marker, rebase after size
   mismatch, permanent-error stop, flush bound) and a live task showing
   bytes in the log dir.
5. **Web UI Logs tab** — Delivers: the Logs tab with offset-based
   incremental fetch, SSE-driven refetch, run picker, follow mode. Depends
   on: (3). Verifiable by: viewing a live task's logs streaming in the UI;
   `pnpm lint`.
6. **Retention** — Delivers: archiver step pruning logs of long-archived
   tasks and orphaned directories. Depends on: (1). Verifiable by: archiver
   unit test with backdated archived tasks and an orphan dir.
7. **Fly volume** — Delivers: `[mounts]` entry in `fly.toml` plus the created
   volume, so logs survive redeploys. Depends on: (3). Verifiable by:
   deploy, write logs, redeploy, logs still readable.

Layers 1–3 are safe to merge with no producer; the driver ships nothing until
layer 4. Old drivers against a new server are unaffected (new RPCs unused);
new drivers against an old server hit `Unimplemented`, which the shipper
treats as permanent and goes quiet — stdio behavior is unchanged either way.

## Trade-offs

- **Raw bytes vs. structured lines.** An earlier revision framed the wire as
  line records with `seq`, `stream` (`driver`/`setup`/`agent`), and `level`
  fields. Dropped in favor of raw bytes + offset: the stream is already
  self-describing text (slog prints `level=…`; setup output looks like
  command output), line framing forced arbitrary per-line truncation and
  JSONL encoding, and the `seq` bookkeeping duplicated what a byte offset
  provides for free — the server file's size is simultaneously the ordering,
  the resume point, and the dedup state, and it survives restarts with
  nothing to rebuild. What's lost is server-side filtering by source/level;
  nobody needs it, and cosmetic highlighting can live in the UI.
- **Flat files vs. a Postgres table.** A `driver_logs` table would get
  transactions, FK cascade, and org scoping for free. Rejected (explicitly —
  logs do not belong in Postgres): log volume dwarfs event volume, bulk
  bytes bloat the primary OLTP database and every backup of it, and
  retention becomes DELETE/vacuum churn instead of `rm -r`. Postgres remains
  the source of truth for semantic state; the store interface keeps the wire
  and handlers identical if the backend ever changes.
- **Filesystem vs. object storage (S3/Tigris).** Object storage would
  survive redeploys without a volume and scale past one machine. Deferred,
  not rejected: it adds a credentialed external dependency the stack doesn't
  have today, and the deployment is a single machine by design. The
  `logstore.Store` interface is shaped so an object-store implementation is
  a drop-in later.
- **New RPCs vs. a `log` event payload.** A sixth `events` arm would reuse
  the timeline plumbing. Rejected: it puts the logs into Postgres, would
  swamp `ListEventsByTask` (which the agent's brief and the timeline both
  read in full), and `wake`/routing semantics are meaningless for log bytes.
  The unified-task-event-stream proposal anticipated exactly this split when
  it deferred a "verbose channel".
- **New RPC vs. reviving `UploadLogs`.** The name is right but the shape is
  wrong: `LogEntry{type, content}` has no run, no offset, no idempotency,
  and the handler's remaining job (report shim) has drop-everything-else
  semantics that would have to change meaning. A clean pair of RPCs is
  simpler than overloading a deprecated wire; retiring `UploadLogs` once the
  report tool moves to a first-class RPC stays orthogonal.
- **Batched unary vs. client-streaming RPC.** A stream saves per-request
  overhead but adds connection-lifetime state, reconnect logic, and a novel
  pattern — everything else in the system (runner events, report uploads) is
  batched unary with retries, and the Web UI reads via SSE-signal + refetch.
  Offset-based unary appends get the same throughput at 2 s latency with
  none of the machinery.
- **Driver-side vs. runner-side capture.** The runner could tail
  `docker logs` and ship without touching the driver, and it would catch even
  driver panics. Rejected as the primary path: it is backend-specific (the
  docker, lambda-microvm, and future firecracker/nomad backends would each
  need a log-tail implementation) and runs against the `driver-owned-events`
  direction of the driver reporting for itself. The RPC deliberately doesn't
  care who the producer is, so a runner-side backstop for crashed drivers
  can be added later.

## Open Questions

1. **Interim run identity.** Until `run-scoped-runner-events` lands, is
   `version` from the driver's initial `GetTask` good enough (it can lag the
   provisioned version if a command raced), or should this proposal wait for
   the runner to pass the version explicitly and ship logs with `version=0`
   grouping in the meantime?
2. **Fly volume vs. accepting redeploy loss.** Is a mounted volume worth the
   operational step, or is "logs survive machine stop/start but not
   redeploys" acceptable until object storage is wanted anyway? The offset
   rebase keeps the pipeline healthy either way.
3. **Limits.** Are 64 KiB/request, a 1 MiB driver buffer, and a 16 MiB
   per-run cap the right defaults, and should the cap be a server flag
   (`--driver-log-cap`) or hardcoded until someone needs otherwise?
4. **Debug-shell runs.** `shell.Serve` runs produce little of interest —
   should the shipper be disabled for `shell_session` tasks, or is uniform
   behavior simpler?
5. **Claude CLI verbosity.** The CLI's stderr is quiet in normal operation.
   Is it worth a workspace-level knob to also ship the raw `stream-json`
   stdout (bounded by the same caps) for deep debugging, or does the
   tool-summary slog line remain the permanent answer?
