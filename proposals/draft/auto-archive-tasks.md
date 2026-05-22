# Auto-Archive Tasks After Configurable Timeout

Issue: https://github.com/icholy/xagent/issues/633

## Problem

Tasks are increasingly being created automatically from workflows (Jira pollers, GitHub webhooks, routing rules, scheduled agents, parent tasks spawning children, etc.) rather than by humans in the UI. Nobody remembers to archive these because no human "owns" them — they reach a terminal status (`COMPLETED`, `FAILED`, `CANCELLED`) and then sit forever with `archived = false`.

The runner's `Prune()` loop only removes containers for **archived** tasks (`internal/runner/runner.go`). Unarchived terminal tasks therefore keep their `xagent-{task-id}` containers around in Docker indefinitely. The result is a resource leak proportional to workflow volume.

We need a per-task auto-archive timeout that lets the system reclaim these containers without anyone having to remember.

## Design

### Overview

Add an `archive_after` interval to each task. Treat "archived" as a value computed at read time: a task is effectively archived if either the persisted `archived` column is `TRUE`, **or** it's in a terminal status past its `archive_after` deadline.

No background worker, no scheduled writes. Every place that currently reads `archived` (the runner's `Prune()`, list/get queries, the API) sees the effective value and behaves exactly as it does today when a human archives a task — including the runner reaping the container.

### Database schema

New migration `20260522000001_task_archive_after.sql`:

```sql
ALTER TABLE tasks
    ADD COLUMN archive_after interval;

-- Helper function: is this task effectively archived right now?
CREATE OR REPLACE FUNCTION task_effective_archived(
    archived boolean,
    status integer,
    command integer,
    updated_at timestamp,
    archive_after interval
) RETURNS boolean AS $$
    SELECT archived
        OR (
            archive_after IS NOT NULL
            AND status IN (5, 6, 7)  -- COMPLETED, FAILED, CANCELLED
            AND command = 0          -- no pending command
            AND updated_at + archive_after < NOW()
        );
$$ LANGUAGE SQL STABLE;
```

`archive_after` is a nullable `interval`:

- `NULL` — never auto-archive (current behaviour; explicit opt-out for tasks a human is following)
- non-null — once `updated_at + archive_after < NOW()` and `status IN (COMPLETED, FAILED, CANCELLED)` with no pending command, queries report the task as archived

`updated_at` is already maintained by every status transition and is the right anchor: it advances on each restart, so a re-run extends the deadline naturally. `interval` (rather than e.g. `archive_at` timestamp) is chosen because the deadline is a property of *how long the task should linger after finishing*, not a fixed wall-clock instant — restarts and command transitions reset the clock for free.

If a client doesn't specify `archive_after` at creation time, the task is created with `NULL` (never auto-archive). Callers that want auto-archive opt in per task.

`task_effective_archived` is a `STABLE` SQL function. It can't be used in a generated column (because `NOW()` isn't `IMMUTABLE`), but it can be inlined by the planner and is cheap to call. Defining it once keeps every query site consistent — no chance of one place forgetting to apply the predicate.

### Proto changes

In `proto/xagent/v1/xagent.proto`:

```proto
import "google/protobuf/duration.proto";

message Task {
  // ... existing fields ...
  // Auto-archive this task once it has been in a terminal status
  // (COMPLETED, FAILED, CANCELLED) for this long. Unset = never auto-archive.
  google.protobuf.Duration archive_after = 15;
}

message CreateTaskRequest {
  // ... existing fields ...
  google.protobuf.Duration archive_after = 6;
}

message UpdateTaskRequest {
  // ... existing fields ...
  // Tri-state: unset = leave alone; zero duration = clear (never archive);
  // positive = set new timeout.
  google.protobuf.Duration archive_after = 6;
}
```

The proto `Task.archived` field continues to carry the **effective** archived value, so clients (Web UI, CLI, runner) don't need to be aware that some tasks are archived implicitly. A separate `archive_after` field exposes the configured interval for display and edit.

### Model changes

In `internal/model/task.go`:

```go
type Task struct {
    // ... existing fields ...
    ArchiveAfter *time.Duration `json:"archive_after,omitempty"`
}
```

`Task.Archived` continues to carry the effective value (populated by the SELECT). The existing helpers (`CanArchive`, `CanUnarchive`, `applyRunnerEventStarted` which cancels an archived task that tries to start, etc.) all keep working as-is — they read `t.Archived` and never need to know whether it came from the column or the predicate.

`ArchiveTask` (manual archive) writes `archived = TRUE` to the column as today. `UnarchiveTask` writes `archived = FALSE` and, to avoid the predicate immediately re-archiving the task, clears `archive_after` as part of the same update. (Captured in Open Questions — there are alternatives.)

### Store changes

Each existing query that reads `archived` is updated to read `task_effective_archived(...) AS archived` instead. Each query that filters on `archived` is updated to filter on the function.

```sql
-- name: GetTask :one
SELECT id, name, parent, runner, workspace, instructions, status, command,
       version, created_at, updated_at,
       task_effective_archived(archived, status, command, updated_at, archive_after) AS archived,
       org_id, archive_after
FROM tasks
WHERE id = $1 AND org_id = $2;

-- name: ListTasks :many
SELECT id, name, parent, runner, workspace, instructions, status, command,
       version, created_at, updated_at,
       task_effective_archived(archived, status, command, updated_at, archive_after) AS archived,
       org_id, archive_after
FROM tasks
WHERE org_id = $1
  AND NOT task_effective_archived(archived, status, command, updated_at, archive_after)
ORDER BY created_at DESC;
```

`ListTasksForRunner` gets the same treatment (and keeps its `command != 0` predicate, which is mutually exclusive with the auto-archive predicate).

`GetTaskForUpdate` is used inside the manual archive/unarchive transactions and **does not** apply the function — those code paths need to read the persisted `archived` column to know whether a manual archive has happened. This is the one place where the raw value matters; everywhere else uses the effective value.

For Postgres planner support, add an index that helps the `ListTasks`/`ListTasksForRunner` filter prune the implicit-archived rows efficiently:

```sql
CREATE INDEX idx_tasks_active_org_created
    ON tasks (org_id, created_at DESC)
    WHERE NOT archived;
```

The implicit-archive predicate is then evaluated only on the (small) set of unarchived terminal tasks with a non-null `archive_after`, which is bounded by recent activity.

### Manual archive / unarchive

- **ArchiveTask** sets `archived = TRUE` as today. Idempotent regardless of whether the task was already implicitly archived.
- **UnarchiveTask** sets `archived = FALSE` and clears `archive_after`. Without clearing the timeout, the task would flip back to implicitly archived on the next read — confusing. Clearing it explicitly says "I want this task back as an active item; don't auto-archive it again."

### CLI changes

`xagent task create` and `xagent task update` accept `--archive-after <duration>` (e.g. `24h`, `30m`). Standard Go `time.ParseDuration` syntax. Omitted at create time = never auto-archive; `0` on update = clear the field.

`xagent task list` already shows enough; we don't add new columns by default. `xagent task show` (if/when added) would surface the field.

### Web UI

The task creation form gets an optional "Auto-archive after" field with a few sensible presets (1h / 24h / 7d / Never). The task detail page shows the current setting and lets it be edited inline.

The UI changes are minimal — most automated tasks set the value via the API at creation time and don't need a human to touch it.

### MCP changes

The xagent MCP server's `create_child_task` tool gets an `archive_after` parameter (string, parsed as `time.Duration`). Agents creating ephemeral helper tasks should pass a short value (e.g. `1h`) so those tasks self-clean.

The `update_my_task` tool also gets `archive_after`, so an agent that knows it's wrapping up can set a short timeout itself.

### Container cleanup

The runner's `Prune()` loop is **unchanged**. It already iterates Docker containers, looks each one up via `GetTask`, and removes stopped containers whose task is archived. Because `GetTask` now returns the effective archived value, expired tasks naturally trigger pruning on the next prune tick (every `--poll` interval — default 5s).

This is the whole point of the design: piggyback on existing machinery instead of adding a parallel writer.

## Trade-offs

| Approach | Pros | Cons |
|----------|------|------|
| **Per-task `archive_after` + read-time predicate** (proposed) | No background worker, no scheduling, no race conditions; eventually consistent for free; runner unchanged | Every read query touching `archived` must use the helper function; UI doesn't get a real-time SSE notification at the moment the deadline passes |
| **Per-task `archive_after` + server worker** (earlier version) | Real-time SSE notification at archive time; per-task audit log entry | New background process to operate; possible duplicate writes under multi-replica server |
| **Runner-side cleanup of unarchived tasks** | No server changes | Conflates "archived" with "cleaned up"; runner reaches into task lifecycle decisions it doesn't own; multiple runners would each need the same policy |
| **Auto-archive on terminal transition (immediately)** | Trivial implementation | Defeats the purpose of `archived = false` as a UI inbox; humans can't review failures before they vanish |
| **Hard-delete instead of archive** | Reclaims disk faster (logs, links, events go too) | Loses audit trail and breaks `parent` references from child tasks; archived rows are cheap to keep |

Choosing the read-time predicate over a worker trades a small loss (no live SSE event when the deadline crosses) for a much simpler operational story: nothing to schedule, nothing to monitor, no race against manual archive/unarchive. The Web UI already refetches on focus and on every poll tick from the runner-driven `change` notifications; users rarely notice the missing edge transition.

`interval` rather than `archive_at` timestamp means restarts and updates extend the deadline naturally (because `updated_at` advances), which matches the intent: "X time after the task is actually done."

## Open Questions

1. **Unarchive semantics.** Proposed: `UnarchiveTask` also clears `archive_after` so the task doesn't flip back to implicitly archived. Alternative: leave `archive_after` alone but reset `updated_at` (so the timer restarts). Or: introduce a second column `archive_after_disabled bool` that the unarchive call sets. The proposed behaviour is simplest; the unarchive flow is rare enough that "you must re-set the timeout if you still want it" feels acceptable.

2. **Should child tasks inherit `archive_after` from their parent?** Right now a parent must pass it explicitly to `create_child_task`. Inheriting would mean a workflow only needs to set the timeout once at the top, which is convenient but introduces non-obvious behaviour. Likely answer: don't inherit; require explicit value, since the callers creating child tasks know their own intent.

3. **What about archived tasks with unarchived children?** A parent archived (manually or implicitly) might still have running or terminal-but-unarchived children. The current archive operation doesn't cascade. Most likely we leave this alone (children get archived independently when their own deadline passes), but should confirm there's no UI assumption that an archived parent implies archived descendants.

4. **Surfacing auto-archived tasks.** Once a task is effectively archived it falls out of `ListTasks`. Users investigating "why did my container disappear" need a way to find these. The persisted `archived` flag is still `FALSE` for implicitly-archived tasks, which is a useful signal — a future "show implicitly archived" filter could query `archive_after IS NOT NULL AND NOT archived AND status IN (...) AND updated_at + archive_after < NOW()`. Out of scope for this proposal but worth noting.
