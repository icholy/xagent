# In-sandbox driver logs at /xagent/log

Issue: https://github.com/icholy/xagent/issues/1241

## Problem

The driver (`xagent driver`) runs inside the sandbox and scatters all of its
diagnostics across the container's stdio, where nothing collects them:

- The driver's own structured `slog` output goes to `slog.Default()`, which the
  driver command never configures — it inherits the process default handler
  writing to `os.Stderr` (`internal/command/driver.go:40`). This is the setup
  progress, the `loaded config` line, the per-tool-call summary lines parsed
  from the Claude stream (`a.log.Info("tool", …)` in
  `internal/agent/claude.go`), and the terminal `task failed` error.
- Setup command stdout/stderr are wired straight to `os.Stdout`/`os.Stderr`
  (`Driver.setup` in `internal/agent/driver.go`).
- The Claude Code CLI's own stderr is passed straight through to the container's
  stderr (`cmd.Stderr = os.Stderr` in `internal/agent/claude.go`).

None of this reaches an operator except by shelling onto the runner host and
running `docker logs xagent-{id}` — and even that is destroyed the moment the
sandbox is pruned or replaced. `Runner.Prune()` removes containers for archived
tasks, and restarts adopt or recreate the container, so a failed run frequently
cannot be debugged post-mortem at all. The Web UI shows only the one-line
`failed` reason (`Sandbox failed: …`) and deliberate `report` events.

## Design

The sandbox already grows a first-class operator entry point: the driver
reverse-shell (`proposals/implemented/driver-reverse-shell.md`). An operator can
`xagent shell <task-id>` into a finished sandbox and get an interactive shell in
the same filesystem the run used. This proposal makes that shell the log viewer:
**the driver tees everything it emits into a file inside the sandbox, and the
operator reads it with the shell they already have.**

No logs are shipped to the server. There is no new server-side storage, no new
RPC, no proto change, and no Web UI change. The sandbox filesystem is the store;
the reverse-shell is the viewer.

### Where logs land: `/xagent/log`

The driver writes run logs under a fixed directory in the container:

```
/xagent/log/run-<version>.log        # the current run's log
/xagent/log/run-<version>.log.1      # rotation backup within a run (if any)
```

`/xagent` is chosen deliberately over the existing config convention
`/tmp/xagent` (`internal/agent/config.go:16`): `/tmp` is frequently a tmpfs or a
directory a setup step may clear, whereas `/xagent` lives on the container's
writable layer, which the runner *preserves across runs* when it adopts an
exited container (`docker.go` `adopt`). Persistence across runs is exactly the
property post-mortem debugging needs. The driver creates the directory at
startup with `os.MkdirAll("/xagent/log", 0o777)` (matching the `0777` the runner
already uses for the config dir so a non-root agent user can also write there);
creation failure is non-fatal and logged — logging to a file is best-effort and
must never take down a run.

### Per-run separation: one file per run version

The same container is reused across runs, and each run has a monotonically
increasing `task.Version` (`proposals/*/task-run-versions.md`). The driver
already fetches the task and reads `version := task.GetVersion()` at the top of
`Driver.Run` (`internal/agent/driver.go:71-72`) before any output of
consequence. The run log is named `run-<version>.log` using that value, so:

- runs never clobber each other's logs — restart run 3 does not overwrite run 2;
- an operator debugging "why did run 2 fail" opens `run-2.log` even though the
  live/last run is a later version;
- reverse-shell runs (which also increment the version) get their own file, but
  a shell run produces almost no driver output, so this costs nothing.

Opening the sink happens between the task fetch and the first emitted event, so
the `started` event and everything after it is captured. The few `slog` lines
that fire before the fetch (SIGTERM handler wiring, the fetch itself) are not —
they carry no run-specific signal and a fetch failure already exits before any
run begins.

### How the tee works

Today the driver logger is `slog.Default()`, passed in via
`command/driver.go:40`. The driver command (not the shared `agent` package)
becomes responsible for building the sink and the logger:

1. **Open a bounded sink.** A single `io.Writer` for the run, `sink`, backed by
   a size-bounded rotating file (see rotation below). All three streams tee into
   this one writer so the file is a single chronological interleaving of
   everything the run produced — the same thing `docker logs` shows today, plus
   the parsed tool summaries.

2. **Driver slog → file + stderr.** Replace the default handler with one writing
   to `io.MultiWriter(os.Stderr, sink)`:

   ```go
   handler := slog.NewTextHandler(io.MultiWriter(os.Stderr, sink), nil)
   driver.Log = slog.New(handler)
   ```

   `os.Stderr` is kept in the multiwriter so `docker logs` keeps working exactly
   as today (see below).

3. **Setup commands → file + stdout/stderr.** In `Driver.setup`, replace the
   direct `os.Stdout`/`os.Stderr` wiring with tees:

   ```go
   c.Stdout = io.MultiWriter(os.Stdout, sink)
   c.Stderr = io.MultiWriter(os.Stderr, sink)
   ```

   This is the highest-value change: setup failures are the most opaque today
   (`setup command N failed: exit status 1` with none of the command's output),
   and now the command's actual stdout/stderr sits in the run log next to the
   error.

4. **Claude CLI stderr → file + stderr.** In `claude.go`, replace
   `cmd.Stderr = os.Stderr` with `cmd.Stderr = io.MultiWriter(os.Stderr, sink)`.
   Claude's stdout is *not* teed raw — it is the newline-delimited JSON stream
   the driver consumes and re-emits as `a.log.Info("tool", …)`/`("text", …)`
   summaries, which already flow through the slog tee in step 2. Teeing the raw
   JSON too would double it and bury the readable summaries.

To thread the sink into the setup and claude stages without touching every
call site, the sink is carried on the `Driver` struct (e.g. a new
`LogSink io.Writer` field defaulting to `io.Discard` when unset, so existing
tests and the non-driver code paths are unaffected) and passed into
`NewAgent`/`Options` for the claude stage. When `LogSink` is `io.Discard` the
tees degrade to plain `os.Stdout`/`os.Stderr` behavior.

### Rotation / size bounds

Two independent limits keep a long-lived, heavily-reused sandbox from filling
its disk:

- **Per-run cap (runaway single run).** The sink is a
  `gopkg.in/natefinch/lumberjack.v2` `*Logger` (already an indirect dependency)
  configured `Filename: /xagent/log/run-<version>.log`, `MaxSize: 128` (MiB),
  `MaxBackups: 1`, `Compress: false`. When a run's log exceeds ~128 MiB
  lumberjack rotates the current file to `run-<version>.log.1` and starts fresh,
  keeping one backup — so a single run is bounded to ~256 MiB and, crucially,
  the *tail* (the output nearest a failure, the most useful part) is always in
  the live `run-<version>.log`. `MaxAge` is left unset; retention is by run, not
  by wall-clock, because a sandbox may sit idle for days between runs.

- **Cross-run retention (accumulating runs).** At startup, after computing the
  run version, the driver prunes old run logs: keep the files for the newest `K`
  run versions (`K = 10`) and delete the rest. Because filenames embed the
  numeric version, this is a directory scan + numeric sort + unlink of the tail,
  including each run's `.log.1` backup. Total on-disk log footprint is therefore
  bounded to roughly `K × 256 MiB` in the pathological case and far less in
  practice.

Both numbers are constants in the driver, not configuration — this is a
debugging aid, not a tunable surface. They can be promoted to config later if a
workspace needs it.

### Interaction with existing docker-logs behavior

`os.Stdout`/`os.Stderr` remain in every tee, so the container's stdio — and
therefore `docker logs xagent-{id}` — behaves byte-for-byte as it does today.
The file is strictly additive: an operator with host access keeps their existing
workflow, and an operator with only `xagent shell` access gains a durable,
per-run, rotation-bounded log they can `cat`, `tail -f`, `less`, or `grep`
inside the sandbox. Nothing about the runner, server, event stream, or Web UI
changes.

### Viewing

No new command. An operator opens a shell into the sandbox and reads the file:

```
xagent shell <task-id>
$ ls /xagent/log
$ less /xagent/log/run-2.log      # the run that failed
$ tail -f /xagent/log/run-3.log   # a live run
```

(`tail -f` on a live run works against a shell opened for the current run; the
common post-mortem case is a terminated run whose file is complete.)

## Implementation Plan

1. **Bounded per-run sink + directory setup** — Delivers: a small
   `internal/agent` helper that, given a run version, `MkdirAll`s `/xagent/log`,
   prunes to the newest `K` run files, and returns a lumberjack-backed
   `io.WriteCloser` for `run-<version>.log` (best-effort: returns `io.Discard`
   and a nil error on filesystem failure). Depends on: nothing. Verifiable by:
   unit tests over a temp dir — file is created, rotation triggers at the size
   cap, and pruning keeps exactly the newest `K` versions.

2. **Driver slog + struct wiring** — Delivers: `command/driver.go` opens the
   sink (from layer 1) after the task fetch, sets `driver.Log` to a handler over
   `io.MultiWriter(os.Stderr, sink)`, and stores the sink on the `Driver`
   struct (default `io.Discard`). Depends on: (1). Verifiable by: running a
   driver against a dummy agent and asserting the driver's `slog` lines appear
   in both stderr and `run-<version>.log`.

3. **Setup command tee** — Delivers: `Driver.setup` tees each setup command's
   stdout/stderr into the sink. Depends on: (2). Verifiable by: a unit/e2e test
   with a failing setup command asserting the command's output lands in the run
   log alongside the `setup command N failed` error.

4. **Claude stderr tee** — Delivers: `Options`/`NewAgent` carry the sink and
   `claude.go` tees the CLI's stderr into it. Depends on: (2). Verifiable by: a
   test asserting Claude CLI stderr reaches the run log while stdout JSON is
   still parsed into tool summaries (and is not duplicated raw).

Layers 3 and 4 are independent of each other and can land in either order once
(2) is in.

## Trade-offs

- **In-sandbox file vs. shipping to the server (the issue's literal ask).** The
  issue proposes shipping logs to the server, persisting them, and rendering
  them in the Web UI. That is strictly more capable — it survives sandbox
  destruction and needs no shell — but it is also much larger: a log-ingest RPC
  (or stream), a storage model and migration, retention/GC on the server, and a
  Web UI log viewer, plus back-pressure and ordering concerns on a hot path.
  This proposal takes the deliberately small path: reuse the reverse-shell that
  already exists as the viewer and the container's own disk as the store. It
  does **not** survive `Prune()` of an archived task or a container recreate — a
  known, accepted limitation (see Open Questions). It closes the most common
  gap (debugging a *failed but not yet archived* run, especially opaque setup
  failures) with a few tees and no new surface.

- **One file per version vs. a single append-only file.** Per-version files make
  "show me run 2" a single `less`, keep rotation reasoning per-run, and make
  cross-run retention a trivial numeric prune. A single file would need internal
  run delimiters and a global size policy that can evict a run mid-stream.

- **lumberjack vs. a hand-rolled bounded writer.** lumberjack is already a
  (transitive) dependency, handles the size-cap + rotation + backup semantics,
  and its "current file holds the newest bytes" behavior is exactly what
  post-mortem debugging wants. A custom ring-buffer writer would preserve the
  tail too but is more code to get right for no benefit.

- **Constants vs. config.** `MaxSize`/`MaxBackups`/`K` are hard-coded to keep the
  change small and the disk bound predictable. They are easy to promote to
  workspace config later if needed.

## Open Questions

- **Pruned/recreated sandboxes are unrecoverable.** When `Runner.Prune()`
  removes an archived task's container, or a backend recreates rather than
  adopts, the run logs die with it — the same failure mode the issue calls out,
  now narrowed to *archived* or *recreated* sandboxes rather than *every*
  sandbox. Is closing the gap for live/finished-but-retained sandboxes enough,
  or is server-side shipping still wanted as a follow-up? This design does not
  preclude it: the same `sink` could later also feed a network shipper.
- **Non-Docker backends.** The plan assumes an ordinary writable container
  filesystem reachable by the reverse-shell. The Lambda MicroVM backend already
  uses `/xagent/...` control-surface paths (`lambdamicrovm.go`); confirm
  `/xagent/log` does not collide and that the microVM's filesystem persists
  across the adopt/reuse path the same way Docker's does.
- **Secret hygiene.** Setup stdout/stderr and Claude stderr may contain
  secrets, and this writes them to disk inside the sandbox. That disk is only
  reachable by an operator who can already `xagent shell` into the sandbox (the
  same trust boundary as running the agent), so the exposure is arguably
  unchanged — but it should be an explicit, accepted decision, not implicit.
