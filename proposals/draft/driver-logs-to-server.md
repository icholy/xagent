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

Driver logs should be shipped to the server, persisted in PostgreSQL, and
viewable in the Web UI.

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
  silently dropped. Its wire shape (`LogEntry{type, content}`) has no run,
  ordering, or source information.
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

## Design

### What counts as a driver log

One row per line, from three sources inside the container, tagged by `stream`:

| `stream` | Source | `level` |
|---|---|---|
| `driver` | The driver's own `slog` records (a tee handler, see below) | `slog` level (`INFO`, `ERROR`, …) |
| `setup`  | Setup command stdout/stderr (`Driver.setup`) | empty |
| `agent`  | The agent CLI's stderr (e.g. Claude Code diagnostics) | empty |

Deliberately **out of scope**:

- The Claude CLI's stdout `stream-json` transcript. It is enormous, embeds
  full file contents, and the driver already distills it into `tool
  name=… summary=…` slog lines (`richer-tool-call-logs`). Those summaries ship
  as `driver` lines; the raw transcript stays in the container.
- The agent's `report` tool output. Reports are agent-authored timeline
  content and stay `report` events. Driver logs are machine diagnostics; the
  two never mix.

The driver keeps writing everything to the container's stdio exactly as today
— shipping is a tee, not a redirect, so `docker logs` still works and a broken
shipper never blinds local debugging.

### Storage: a new `driver_logs` table

Log lines are high-volume machine diagnostics with different write rates, read
patterns, and retention than the semantic `events` stream, so they get their
own table rather than a sixth event payload (see Trade-offs):

```sql
-- migrate:up
CREATE TABLE driver_logs (
    id BIGSERIAL PRIMARY KEY,
    task_id bigint NOT NULL,
    org_id bigint NOT NULL,
    version bigint NOT NULL DEFAULT 0,        -- run identity (task.Version at provisioning)
    seq bigint NOT NULL,                      -- driver-assigned, monotonic per run
    stream text NOT NULL,                     -- 'driver' | 'setup' | 'agent'
    level text NOT NULL DEFAULT '',           -- slog level for stream='driver', else ''
    line text NOT NULL,                       -- single line, truncated to 8 KiB
    created_at timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_driver_logs_task_id FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    CONSTRAINT fk_driver_logs_org_id FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
    CONSTRAINT uq_driver_logs_run_seq UNIQUE (task_id, version, seq)
);

CREATE INDEX idx_driver_logs_task_id_id ON driver_logs (task_id, id);

-- migrate:down
DROP TABLE driver_logs;
```

- `(task_id, version, seq)` is unique so at-least-once delivery is idempotent:
  the insert is `ON CONFLICT DO NOTHING` and a retried batch is harmless.
- `id` (like `events.id`) defines global read order and serves as the UI's
  incremental-fetch cursor; `(version, seq)` preserves the driver's own order
  even if batches land out of order.
- Structured `slog` attrs are rendered into the line text (the default
  `key=value` text form), not stored as jsonb. Nobody queries driver logs by
  attribute; a text line keeps the table small and the UI trivial.

### Wire: two new RPCs on `XAgentService`

```proto
message DriverLogLine {
  int64 seq = 1;                              // per-run monotonic, driver-assigned
  string stream = 2;                          // "driver", "setup", "agent"
  string level = 3;                           // slog level for stream="driver", else ""
  string line = 4;                            // single line, max 8 KiB
  google.protobuf.Timestamp created_at = 5;   // driver-side timestamp, informational
}

message SubmitDriverLogsRequest {
  int64 task_id = 1;
  int64 version = 2;                          // run identity; 0 until run-scoped-runner-events lands
  repeated DriverLogLine lines = 3;           // max 500 per request
}

message SubmitDriverLogsResponse {
  // True once the server has hit the per-run line cap and is discarding
  // further lines. The driver stops shipping (but keeps writing to stdio).
  bool capped = 1;
}

message ListDriverLogsRequest {
  int64 task_id = 1;
  int64 after_id = 2;                         // cursor: rows with id > after_id
  int32 limit = 3;                            // default 1000, max 1000
}

message DriverLog {
  int64 id = 1;
  int64 version = 2;
  int64 seq = 3;
  string stream = 4;
  string level = 5;
  string line = 6;
  google.protobuf.Timestamp created_at = 7;
}

message ListDriverLogsResponse {
  repeated DriverLog logs = 1;
}
```

- **`SubmitDriverLogs`** follows the `SubmitRunnerEvents` /
  `UploadLogs` handler shape: coarse `AllowOp(OpTaskWrite)` gate, load the
  task, instance check `Allow(OpTaskWrite, task.ScopeAttr()...)`. The
  task-scoped JWT from `CreateTaskToken` already satisfies this for the
  driver's own task and nothing else; archiving the task revokes it via the
  `task.archived` predicate. No new scope ops.
- Server-side enforcement, independent of driver behavior: ≤ 500
  lines/request, each line truncated to 8 KiB, and a per-run cap (default
  20,000 lines). Past the cap the handler inserts one final
  `[xagent] log cap reached, dropping further lines` marker row and returns
  `capped=true`.
- The handler publishes a `change` notification with a
  `{Action: "appended", Type: "driver_logs", ID: task_id}` resource — same
  pattern `UploadLogs` uses with `task_logs` — so the Web UI's existing SSE
  wake-and-refetch loop picks it up. Batching (below) naturally throttles this
  to at most one notification per flush.
- **`ListDriverLogs`** is read-side for the Web UI: `OpTaskRead` +
  `task.ScopeAttr()`, keyset pagination on `id`. No streaming RPC — the UI
  convention is SSE signal + refetch, and the cursor makes refetch cheap.

### Driver-side capture and shipping

A new `internal/agent/logship` package owns buffering and delivery:

```go
type Shipper struct { /* … */ }

func New(opts Options) *Shipper                       // Client, TaskID, Version, Log
func (s *Shipper) Handler(next slog.Handler) slog.Handler // tee: next + buffer (stream="driver")
func (s *Shipper) Writer(stream string) io.Writer     // line-splitting tee target
func (s *Shipper) Run(ctx context.Context)            // flush loop, exits when ctx done
func (s *Shipper) Flush(ctx context.Context) error    // final drain, bounded by ctx
```

- **Capture points** (all tees; container stdio is unchanged):
  - `command/driver.go` builds
    `slog.New(shipper.Handler(slog.Default().Handler()))` and hands it to the
    driver and agent.
  - `Driver.setup` sets `c.Stdout`/`c.Stderr` to
    `io.MultiWriter(os.Stdout, shipper.Writer("setup"))` (and stderr
    respectively).
  - `claude.go` (and the other agent providers) set
    `cmd.Stderr = io.MultiWriter(os.Stderr, shipper.Writer("agent"))`.
  - `Writer` splits on newlines, flushing a partial line if it exceeds the
    8 KiB line limit or on final `Flush`.
- **Batching**: appends go into an in-memory ring buffer (cap 5,000 lines). A
  single flush goroutine sends one in-flight `SubmitDriverLogs` at a time —
  whenever lines are pending and at most every 2 s — so ordering is preserved
  without sequencing machinery beyond `seq`.
- **Backpressure**: log writes never block on the network. If the buffer is
  full (server slow or unreachable) the oldest lines are dropped and a counter
  incremented; the next successful flush prepends a synthetic
  `[xagent] dropped N lines` line (with its own `seq`) so gaps are visible.
- **Retry**: transient RPC errors back off and retry the same batch (the
  unique key makes redelivery idempotent). Permanent errors
  (`PermissionDenied` — e.g. task archived, `NotFound`, `InvalidArgument`) or
  `capped=true` stop the shipper for the rest of the run; stdio output
  continues untouched. This mirrors the `isPermanentError` classification in
  `internal/runner/eventoutbox.go`.
- **Final flush**: `Driver.Run` already keeps `eventCtx` alive through
  SIGTERM for the terminal `SubmitRunnerEvents`. Before that terminal submit
  it calls `shipper.Flush(eventCtx)` with a short bound (~5 s). The flush is
  best-effort: a failure is logged and does **not** affect the exit code —
  the "did I report?" bit stays exclusively about the lifecycle event.
- **Run identity**: the driver fills `version` from the `GetTask` response it
  already fetches in `Driver.run` (lines buffered before that flush with the
  version applied at send time). Once `run-scoped-runner-events` lands and the
  runner passes the provisioned version explicitly, the shipper uses that
  instead — same value, delivered earlier and immune to mid-run command bumps.

### No durable outbox in the container

The runner's lifecycle events go through a durable filesystem outbox because
the runner host outlives the events. The driver's filesystem **is** the
sandbox: any spooled log files die with the container, exactly when the logs
would be needed. A durable outbox buys nothing here — logs are best-effort
in-memory, and correctness-critical reporting stays on `SubmitRunnerEvents`.

### Web UI

- The task page (`webui/src/routes/tasks.$id.tsx`) gains a **Logs** tab
  beside the timeline: monospace, virtualized list, level-colored `driver`
  lines, `stream` badge, divider rows between run `version` groups, and a
  follow toggle.
- Data comes from `ListDriverLogs` via connect-query, refetched when the SSE
  `change` notification carries a `driver_logs` resource for the task —
  incremental via `after_id`, so a follow session only transfers new rows.
- The timeline is unchanged. `report` events remain the agent's authored
  output in the conversation view; driver logs are a separate diagnostic
  surface, visually distinct (terminal styling vs. timeline cards), so the
  two cannot be confused.

### Retention

- `ON DELETE CASCADE` ties log lifetime to the task row, same as events.
- Archived tasks keep their logs for a grace window: the existing archiver
  loop (`internal/server/archiver`) gains a step that deletes `driver_logs`
  rows for tasks archived more than 7 days ago (batch-limited per tick, like
  `Tick()`'s task batching). Live and recently-archived tasks stay fully
  debuggable; the table cannot grow without bound.
- The per-run cap (20,000 lines × 8 KiB worst case) bounds a single runaway
  run at ~160 MB, and in practice runs are orders of magnitude smaller.

## Failure Modes

- **Driver crash / OOM / SIGKILL**: lines since the last flush (≤ 2 s of
  output, plus anything the network delayed) are lost. The runner's
  `supervise` backstop still emits `failed`. The shipped prefix usually
  contains the interesting part; a future runner-side backstop could
  `docker logs --tail` on abnormal exit and submit through the same RPC (the
  request shape is producer-agnostic), but that is out of scope here.
- **Server unreachable / socket errors**: the ring buffer absorbs up to 5,000
  lines, then drops oldest with a visible `dropped N lines` marker. The agent
  is never blocked or failed because of log delivery.
- **Task archived mid-run**: the token's `task.archived:"false"` predicate
  turns submits into `PermissionDenied`; the shipper classifies it permanent
  and goes quiet, matching how the sandbox is about to be pruned anyway.
- **Retry duplicates**: `ON CONFLICT (task_id, version, seq) DO NOTHING`
  makes redelivery invisible.
- **Restart while old run still flushing**: the old driver's lines carry the
  old `version`, the new run's lines the new one — no interleaving in the
  grouped UI, no clobbering. (With `version=0` before run-scoped-runner-events
  lands, lines from both runs share a group; acceptable during the interim,
  see Open Questions.)
- **Log flood**: per-line truncation, per-request limit, buffer cap, and the
  server-side per-run cap fail in that order; every drop point leaves a
  marker. The failure mode is "logs get sparse", never "task fails" or
  "server tips over".

## Implementation Plan

1. **Schema migration** — Delivers: the `driver_logs` table, constraints, and
   index. Depends on: nothing. Verifiable by: migration runs cleanly up and
   down (`mise run up:test`).
2. **Proto + generated code** — Delivers: `SubmitDriverLogs` /
   `ListDriverLogs` messages and RPCs, `mise run generate` output, moq
   regeneration. Depends on: nothing (mergeable before 1; handlers don't
   exist yet). Verifiable by: build + generated-code diff review.
3. **Store methods** — Delivers: `CreateDriverLogs` (batch insert, conflict-
   ignoring, cap-aware) and `ListDriverLogs` (keyset) in `internal/store`.
   Depends on: (1). Verifiable by: store unit tests against the test
   database.
4. **Server handlers** — Delivers: the two handlers in
   `internal/server/apiserver` with scope checks, limits, the cap marker, and
   the `driver_logs` change notification. Depends on: (2), (3). Verifiable
   by: handler tests following `log_test.go` / `runner_test.go` patterns
   (permissions, truncation, cap, idempotent retry).
5. **Driver shipper** — Delivers: `internal/agent/logship` (ring buffer,
   flush loop, tee handler, line writer) plus wiring in `command/driver.go`,
   `Driver.setup`, and the agent providers' stderr; final flush in
   `Driver.Run`. Depends on: (2), (4). Verifiable by: unit tests with the moq
   client (ordering, drop marker, permanent-error stop, flush bound) and a
   live task showing rows in the table.
6. **Web UI Logs tab** — Delivers: the Logs tab with incremental fetch,
   SSE-driven refetch, run grouping, follow mode. Depends on: (4). Verifiable
   by: viewing a live task's logs streaming in the UI; `pnpm lint`.
7. **Retention** — Delivers: archiver step pruning logs of long-archived
   tasks. Depends on: (1). Verifiable by: archiver unit test with backdated
   archived tasks.

Layers 1–4 are safe to merge with no producer; the driver ships nothing until
layer 5. Old drivers against a new server are unaffected (new RPCs unused);
new drivers against an old server hit `Unimplemented`, which the shipper
treats as permanent and goes quiet — stdio behavior is unchanged either way.

## Trade-offs

- **New table vs. a `log` event payload.** A sixth `events` arm would reuse
  storage, RPCs, and the timeline. Rejected: log volume is 100–1000× event
  volume and would swamp `ListEventsByTask` (which the agent's brief and the
  timeline both read in full), `wake`/routing semantics are meaningless for
  log lines, and retention diverges (events are the permanent record; logs
  are prunable diagnostics). The unified-task-event-stream proposal
  anticipated exactly this split when it deferred a "verbose channel".
- **New RPC vs. reviving `UploadLogs`.** The name is right but the shape is
  wrong: `LogEntry{type, content}` has no run, no ordering, no idempotency
  key, and the handler's remaining job (report shim) has drop-everything-else
  semantics that would have to change meaning. A clean pair of RPCs is
  simpler than overloading a deprecated wire; retiring `UploadLogs` once the
  report tool moves to a first-class RPC stays orthogonal.
- **Batched unary vs. client-streaming RPC.** A stream saves per-request
  overhead but adds connection-lifetime state, reconnect logic, and a novel
  pattern — everything else in the system (runner events, report uploads) is
  batched unary with retries, and the Web UI reads via SSE-signal + refetch.
  Batched unary with an idempotency key gets the same throughput at 2 s
  latency with none of the machinery.
- **Driver-side vs. runner-side capture.** The runner could tail
  `docker logs` and ship without touching the driver, and it would catch even
  driver panics. Rejected as the primary path: it is backend-specific (the
  docker, lambda-microvm, and future firecracker/nomad backends would each
  need a log-tail implementation), loses structure (everything collapses into
  one byte stream), and runs against the `driver-owned-events` direction of
  the driver reporting for itself. The RPC deliberately doesn't care who the
  producer is, so a runner-side backstop for crashed drivers can be added
  later.
- **Rendered text vs. structured jsonb attrs.** Storing slog attrs as jsonb
  would allow attribute queries, but no consumer needs them: the UI renders
  lines, and grep-shaped debugging works on text. Text keeps rows small and
  the schema stable across attr changes.

## Open Questions

1. **Interim run identity.** Until `run-scoped-runner-events` lands, is
   `version` from the driver's initial `GetTask` good enough (it can lag the
   provisioned version if a command raced), or should this proposal wait for
   the runner to pass the version explicitly and ship logs with `version=0`
   grouping in the meantime?
2. **Retention numbers.** Are 7-days-after-archive and a 20,000-line per-run
   cap the right defaults, and should they be server flags
   (`--driver-log-retention`, `--driver-log-cap`) or hardcoded until someone
   needs otherwise?
3. **Debug-shell runs.** `shell.Serve` runs produce little of interest —
   should the shipper be disabled for `shell_session` tasks, or is uniform
   behavior simpler?
4. **Claude CLI verbosity.** The CLI's stderr is quiet in normal operation.
   Is it worth a workspace-level knob to also ship the raw `stream-json`
   stdout (heavily truncated) for deep debugging, or does the tool-summary
   slog line remain the permanent answer?
