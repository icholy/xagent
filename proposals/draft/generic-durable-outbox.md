# Generic Durable Outbox

Issue: https://github.com/icholy/xagent/issues/1179

## Problem

The runner's `EventQueue` (`internal/runner/eventqueue.go`) buffers `SubmitRunnerEvents`
calls that fail and retries them on the next `Drain`. It exists to ride out transient
connection issues between the runner and the server. It has two structural limitations:

1. **Not durable.** The queue is an in-memory `container/list.List`
   (`eventqueue.go:24`). Events that are enqueued but not yet acknowledged are lost if the
   runner crashes or restarts. That restart-during-outage window is precisely the case the
   queue exists to protect, so a lost `started`/`stopped`/`failed` event leaves the
   server's task state permanently out of sync. The runner already keeps crash-safe on-disk
   state for the task→sandbox mapping (`internal/runner/taskstate`), but the buffer that
   guards state transitions does not.

2. **Not reusable.** The queue is hardcoded to `model.RunnerEvent` and to the
   `SubmitRunnerEvents` RPC (`eventqueue.go:71-74`). Other outbound, failure-prone
   operations want the same "persist, then deliver, then retry with backoff" guarantee but
   cannot use `EventQueue` as written.

The goal is a generic, durable outbox: persist a message before attempting delivery,
survive restarts, retry with backoff, and be parameterized over both the message type and
the delivery mechanism. The runner's event delivery becomes its first consumer.

## Design

A new leaf package `internal/x/outbox` provides a durable, at-least-once outbox generic
over a payload type `T`. It is generic on two axes:

- **Payload type** — `Outbox[T]` carries any JSON-serializable `T` (e.g.
  `model.RunnerEvent`).
- **Backing store** — persistence is behind a small `Store` interface. A filesystem
  implementation ships now (mirroring `taskstate`); a Postgres implementation can be added
  later for server-side consumers without touching the engine or existing callers.

### Delivery semantics

At-least-once, head-of-line-blocking FIFO — preserving the current `EventQueue` contract:

1. `Enqueue` durably persists the message, then wakes the drain loop.
2. `Run` delivers persisted messages in enqueue order (FIFO).
3. On success, the message is removed from the store.
4. On a **transient** error, delivery stops at that message and retries after a backoff;
   later messages stay blocked behind it (matching `Drain`'s current behaviour at
   `eventqueue.go:75-82`).
5. On a **permanent** error, the message is moved to a dead-letter area and delivery
   proceeds to the next message (today `Drain` logs and drops it at `eventqueue.go:76-79`;
   dead-lettering is strictly safer — nothing is silently lost).

Because a message is persisted *before* delivery and removed *after* the delivery call
returns success, a crash between the server's commit and the local remove causes
redelivery. Consumers must therefore be idempotent. This is already true for the runner:
the server's `SubmitRunnerEvents` handler commits its transaction before returning
(`internal/server/apiserver/runner.go`), and the accepted `driver-owned-events` proposal
establishes that duplicate runner events are safe by design.

### Store interface

```go
package outbox

// Record is one persisted, undelivered message. Seq is a per-outbox monotonic
// sequence number that defines delivery (FIFO) order. Payload is the opaque,
// JSON-encoded T; the store never decodes it.
type Record struct {
	Seq     uint64          `json:"seq"`
	Payload json.RawMessage `json:"payload"`
}

// Store is the durable, crash-safe backing store for an outbox. Implementations
// must be safe for concurrent use.
type Store interface {
	// Append durably persists payload under a new, strictly increasing Seq and
	// returns the assigned Seq. It must not return until the record is durable.
	Append(payload json.RawMessage) (uint64, error)
	// List returns all undelivered records in ascending Seq order.
	List() ([]Record, error)
	// Remove deletes the record with the given Seq. It is idempotent.
	Remove(seq uint64) error
	// DeadLetter atomically moves the record with the given Seq out of the live
	// set into the dead-letter area.
	DeadLetter(seq uint64) error
}
```

#### Filesystem implementation (`outbox.FileStore`)

Directly reuses the proven `taskstate` pattern (`taskstate.go:52-95`): one atomic file per
record.

- Layout: `<dir>/<seq>.json` for live records, `<dir>/dead/<seq>.json` for dead-lettered
  ones, where `<seq>` is a 20-digit zero-padded `uint64` so lexical filename order equals
  numeric `Seq` order.
- `Append`: pick the next `Seq` (in-memory counter seeded at `Open` from the max existing
  filename across both the live and dead directories, so sequence numbers never repeat even
  after dead-lettering), marshal, then write via temp-file + `fsync` + atomic `rename`
  (identical to `taskstate.Store.Write`).
- `List`: `os.ReadDir`, ignore temp files and non-`<uint64>.json` names (as
  `taskstate.parseRecordName` does at `taskstate.go:165-177`), decode, sort by `Seq`.
- `DeadLetter`: `rename` `<dir>/<seq>.json` → `<dir>/dead/<seq>.json`.

No new dependency: the filesystem store is stdlib-only, exactly like `taskstate`.

### Outbox engine

```go
package outbox

type Options[T any] struct {
	Store   Store
	Deliver func(ctx context.Context, msg T) (permanent bool, err error) // the RPC/HTTP call
	Backoff backoff.BackOff                                              // retry policy; Reset on success, NextBackOff per failure
	Log     *slog.Logger
}

type Outbox[T any] struct { /* store, wakeup.Chan, opts */ }

func New[T any](opts Options[T]) *Outbox[T]

// Enqueue durably persists msg, then wakes Run. It returns an error only if the
// message could not be persisted.
func (o *Outbox[T]) Enqueue(msg T) error

// Run delivers persisted messages until ctx is cancelled.
func (o *Outbox[T]) Run(ctx context.Context)

// Len reports the number of undelivered messages.
func (o *Outbox[T]) Len() (int, error)
```

`Run` is the durable analogue of today's `Run`/`Drain` pair (`eventqueue.go:66-120`): on
wakeup it `List`s the store, delivers each record in `Seq` order via `Deliver`, and
`Remove`s it on success. `Deliver` reports whether a failure is permanent as its first
return value, so the code that already holds the error classifies it inline — no separate
predicate. A transient error (`permanent == false`) sleeps for `Backoff.NextBackOff()` (a
`backoff.BackOff` from the already-vendored `github.com/cenkalti/backoff/v5`, replacing the
current fixed `retryInterval`) and retries from the same record; a permanent error
(`permanent == true`) triggers `DeadLetter` and continues. `Run` calls `Backoff.Reset()`
whenever the store fully drains, so each new failure streak starts from the initial interval.
On startup, `Run`'s first pass
naturally redelivers everything `List` returns — this is the durability payoff, and it
requires no separate recovery path.

`Backoff` defaults to a capped exponential policy when nil; passing
`backoff.NewConstantBackOff(interval)` reproduces the current fixed-interval behaviour for a
drop-in match.

### Runner adoption (first consumer)

`internal/runner/eventqueue.go` is deleted and replaced by a thin construction of
`outbox.Outbox[model.RunnerEvent]` in `internal/command/runner.go` (currently
`runner.go:143-147`, `runner.go:221`):

```go
ob := outbox.New[model.RunnerEvent](outbox.Options[model.RunnerEvent]{
	Store: outboxStore, // outbox.FileStore under the runner state dir, next to taskstate
	Deliver: func(ctx context.Context, ev model.RunnerEvent) (permanent bool, err error) {
		_, err = client.SubmitRunnerEvents(ctx, &xagentv1.SubmitRunnerEventsRequest{
			Events: []*xagentv1.RunnerEvent{ev.Proto()},
		})
		return isPermanentError(err), err
	},
	Log: log,
})
...
go ob.Run(ctx)
```

The five `queue.Enqueue(...)` call sites in `internal/runner/runner.go` (lines 170, 194,
233, 302, 305) keep the same `Enqueue(model.RunnerEvent)` shape. `Enqueue` now returns an
error (persistence can fail); the runner logs it, exactly as other local-IO failures are
handled today. `isPermanentError` and its Connect-code set (`eventqueue.go:91-98`) move into
the runner wiring unchanged.

The outbox directory lives under the runner's existing state directory (the parent of the
`taskstate` dir), so it participates in the same lifecycle and cleanup as task state.

### Reuse candidates (motivating "other places")

The generic engine + pluggable `Store` is what makes these tractable later; they are **not**
in scope for this proposal, only enumerated to validate the shape:

- **Driver → server events** (`internal/agent/driver.go`) — the in-container driver makes
  the same `SubmitRunnerEvents` call and could use an `Outbox[model.RunnerEvent]` over a
  container-local `FileStore`.
- **Server-side notification publishing** (`internal/server/apiserver/runner.go`,
  `internal/pubsub`) — a fan-out that today is best-effort after the DB commit; a Postgres
  `Store` implementation would let the server enqueue notifications transactionally and
  deliver them durably. This is the concrete motivation for keeping `Store` an interface.

### No proto changes

This is an internal reliability change. `SubmitRunnerEvents` and every other RPC are
untouched; the wire format and server handlers are unchanged.

## Implementation Plan

A layer cake of small, independently reviewable PRs. Each foundational layer is safe to
merge before the ones above it land.

1. **Outbox store interface + filesystem implementation** — Delivers: the `outbox` package
   with the `Store` interface, `Record`, and `FileStore` (atomic per-record files, seq
   ordering, dead-letter dir), ported from the `taskstate` pattern. No engine yet.
   Depends on: nothing. Verifiable by: unit tests over a temp dir — append/list ordering,
   idempotent remove, dead-letter move, seq monotonicity across restart (re-`Open` the dir),
   and ignoring of temp/garbage files.

2. **Outbox engine** — Delivers: `Outbox[T]`, `Options[T]`, `Enqueue`/`Run`/`Len`, backoff
   on transient errors, dead-letter on permanent errors. Depends on: (1). Verifiable by:
   unit tests with an in-memory fake `Store` and a scripted `Deliver` func — FIFO order,
   head-of-line blocking on transient failure, dead-letter + continue on permanent failure,
   redelivery of a pre-seeded store on startup, backoff timing.

3. **Runner adoption** — Delivers: delete `internal/runner/eventqueue.go`, wire
   `outbox.Outbox[model.RunnerEvent]` + a `FileStore` in `internal/command/runner.go`, move
   `isPermanentError`, keep the `Enqueue` call sites. Depends on: (2). Verifiable by:
   porting the existing `eventqueue_test.go` cases to the new wiring and a runner
   integration test asserting events survive a simulated restart (enqueue → drop the
   in-memory outbox → re-`Open` the dir → deliver).

Later, out of scope here: a Postgres `Store` implementation and the server/driver consumers
above, each a self-contained follow-up that depends only on layers (1)–(2).

## Trade-offs

- **Filesystem store vs. embedded DB (SQLite/bbolt).** The filesystem store adds zero
  dependencies and reuses a pattern already trusted for runner state (`taskstate`), and the
  outbox's access pattern (append, list-in-order, delete-head, small N) is exactly what that
  pattern serves well. An embedded DB would give indexed queries and transactions we don't
  need here; it's deferred behind the `Store` interface if a consumer ever needs it.

- **Head-of-line blocking retained.** Keeping strict FIFO with head-of-line blocking matches
  the current `EventQueue` contract and the ordering guarantees the runner's task state
  machine relies on. Per-key parallel delivery would improve throughput but change
  semantics; it's deliberately out of scope.

- **At-least-once, not exactly-once.** Persist-before-deliver plus remove-after-ack makes
  duplicates possible on crash. Exactly-once would require delivery de-duplication on the
  server. Since runner event consumers are already idempotent (per `driver-owned-events`),
  at-least-once is the right cost/benefit point.

- **Dead-letter vs. drop.** The current queue logs-and-drops permanent errors. Moving them to
  a dead-letter directory instead is strictly safer (nothing vanishes) at the cost of a
  little disk and an eventual cleanup story.

## Open Questions

- **Dead-letter retention.** Do dead-lettered records need a retention/cleanup policy
  (age-based prune, or a `report` to the server), or is manual inspection enough initially?

- **Backoff defaults.** What cap and initial interval should the default exponential policy
  use? The current runner uses a fixed `--poll` interval (default 30s); a sensible default
  might be initial 1s, factor 2, cap 30s.

- **Enqueue failure policy.** If `Enqueue`'s durable write fails (disk full), the runner
  currently has no fallback. Should it fall back to a best-effort in-memory hold, or is
  logging + relying on runner reconciliation (`Runner.Load`) on the next restart sufficient?
