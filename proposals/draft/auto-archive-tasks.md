# Auto-Archive Tasks After Configurable Timeout

Issue: https://github.com/icholy/xagent/issues/633

## Problem

Tasks are increasingly being created automatically from workflows (Jira pollers, GitHub webhooks, routing rules, scheduled agents, parent tasks spawning children, etc.) rather than by humans in the UI. Nobody remembers to archive these because no human "owns" them — they reach a terminal status (`COMPLETED`, `FAILED`, `CANCELLED`) and then sit forever with `archived = false`.

The runner's `Prune()` loop only removes containers for **archived** tasks (`internal/runner/runner.go`). Unarchived terminal tasks therefore keep their `xagent-{task-id}` containers around in Docker indefinitely. The result is a resource leak proportional to workflow volume.

We need a per-task auto-archive timeout that lets the system reclaim these containers without anyone having to remember.

## Design

### Overview

Add an `archive_after` interval to each task. After the task enters a terminal status, a server-side background worker archives it once `updated_at + archive_after < NOW()`. The existing `Prune()` loop on the runner then removes the container as it already does today. No runner changes are required.

Per-task configuration lets transient automated tasks be archived in minutes, while interactive or human-followed tasks can opt out (or be kept for days).

### Database schema

New migration `20260522000001_task_archive_after.sql`:

```sql
ALTER TABLE tasks
    ADD COLUMN archive_after interval;

-- Index supports the auto-archive worker query: scan small set of candidates
-- without table-scanning every archived row.
CREATE INDEX idx_tasks_auto_archive ON tasks (updated_at)
    WHERE archived = FALSE
      AND archive_after IS NOT NULL
      AND status IN (5, 6, 7); -- COMPLETED, FAILED, CANCELLED
```

`archive_after` is a nullable `interval`:

- `NULL` — never auto-archive (current behaviour; explicit opt-out for tasks a human is following)
- non-null — archive once `updated_at + archive_after < NOW()` and `status IN (COMPLETED, FAILED, CANCELLED)`

`updated_at` is already maintained by every status transition and is the right anchor: it advances on each restart, so a re-run extends the deadline naturally. `interval` (rather than e.g. `archive_at` timestamp) is chosen because the deadline is a property of *how long the task should linger after finishing*, not a fixed wall-clock instant — restarts and command transitions reset the clock for free.

### Org-level default

A new `tasks_archive_after` column on the `org_settings`-equivalent (the existing `orgs` table or `GetOrgSettings` source — see `internal/server/apiserver/org.go`) supplies the default that's copied into each new task at creation time when the client doesn't specify one. This gives operators one knob to set sensible behaviour for workflow-created tasks without having to update every integration.

```sql
ALTER TABLE orgs ADD COLUMN default_archive_after interval;
```

The default for the column is `NULL` (preserving current behaviour). Operators opt in by setting it (e.g. `'24 hours'`).

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

message GetOrgSettingsResponse {
  // ... existing fields ...
  google.protobuf.Duration default_archive_after = 6;
}

message SetOrgSettingsRequest {
  google.protobuf.Duration default_archive_after = 1;
}

message SetOrgSettingsResponse {}
```

A new RPC `SetOrgSettings` is added (the existing `GetOrgSettingsResponse` lacks a writer for most fields — this proposal only mutates `default_archive_after`).

### Model changes

In `internal/model/task.go`:

```go
type Task struct {
    // ... existing fields ...
    ArchiveAfter *time.Duration `json:"archive_after,omitempty"`
}
```

A helper on `Task` decides eligibility for auto-archive (kept next to `CanArchive`):

```go
func (t *Task) ShouldAutoArchive(now time.Time) bool {
    if !t.CanArchive() || t.ArchiveAfter == nil {
        return false
    }
    return now.After(t.UpdatedAt.Add(*t.ArchiveAfter))
}
```

`ShouldAutoArchive` reuses `CanArchive()` so it can never violate the existing state-machine rules (e.g. it won't archive a task that still has a pending `command`).

### Store changes

New query in `internal/store/sql/queries/task.sql`:

```sql
-- name: ListAutoArchiveCandidates :many
SELECT id, name, parent, runner, workspace, instructions, status, command,
       version, created_at, updated_at, archived, org_id, archive_after
FROM tasks
WHERE archived = FALSE
  AND archive_after IS NOT NULL
  AND status IN (5, 6, 7)
  AND command = 0
  AND updated_at + archive_after < NOW()
ORDER BY updated_at
LIMIT $1;
```

`LIMIT` keeps the worker tick bounded; the index above makes this an index-only-friendly scan over the active candidate set.

`CreateTask` / `UpdateTask` SQL is extended to include `archive_after`.

### Background worker

A new package `internal/server/archiver/` runs alongside the API server, started from `internal/command/server.go`. It exposes a single goroutine driven by a `time.Ticker`:

```go
type Archiver struct {
    log       *slog.Logger
    store     *store.Store
    publisher pubsub.Publisher
    interval  time.Duration // tick interval, e.g. 1 minute
    batch     int           // max tasks archived per tick, e.g. 100
}

func (a *Archiver) Run(ctx context.Context) error {
    t := time.NewTicker(a.interval)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-t.C:
            if err := a.tick(ctx); err != nil {
                a.log.Error("auto-archive tick failed", "error", err)
            }
        }
    }
}
```

Each tick:

1. Fetches up to `batch` candidates via `ListAutoArchiveCandidates`.
2. For each candidate, opens a transaction, re-reads with `GetTaskForUpdate`, re-checks `ShouldAutoArchive` (defends against races with concurrent restarts), calls `task.Archive()`, persists, writes an audit log row (`Type: "audit"`, `Content: "auto-archived after <duration>"`), commits.
3. Publishes a `change` notification per archived task using the existing `pubsub.Publisher`, so the Web UI's SSE stream updates in real time exactly as if a human clicked Archive.

The audit log entry keeps the existing UI surface honest — users browsing a task's log can see why it disappeared from their list.

The candidate query uses a partial index, so even with a high tick rate the worker is cheap. A 1-minute interval is plenty given that the smallest sensible timeout is on the order of minutes; faster ticks add load without helping.

Server runs as a single process today, so no leader election is needed. If we ever scale to multiple server replicas, the worker can be gated by a Postgres advisory lock (`pg_try_advisory_lock`) — noted under Open Questions.

### Server CLI flag

`xagent server` gets a flag to control the worker — primarily for tuning, not for end users:

```
--archive-tick duration   How often to scan for auto-archive candidates (default 1m)
```

Disabling the worker entirely is done by setting the flag to `0`. Users control behaviour via the per-task / per-org setting; this flag is operator-only.

### CLI changes

`xagent task create` and `xagent task update` accept `--archive-after <duration>` (e.g. `24h`, `30m`). Standard Go `time.ParseDuration` syntax. Omitted = use org default; `0` = never archive.

`xagent task list` already shows enough; we don't add new columns by default. `xagent task show` (if/when added) would surface the field.

### Web UI

The task creation form gets an optional "Auto-archive after" field with a few sensible presets (1h / 24h / 7d / Never). The task detail page shows the current setting and lets it be edited inline. The Org Settings page gets a default value field.

The UI changes are minimal — most automated tasks set the value via the API at creation time and don't need a human to touch it.

### MCP changes

The xagent MCP server's `create_child_task` tool gets an `archive_after` parameter (string, parsed as `time.Duration`). Agents creating ephemeral helper tasks should pass a short value (e.g. `1h`) so those tasks self-clean.

The `update_my_task` tool also gets `archive_after`, so an agent that knows it's wrapping up can set a short timeout itself rather than relying on the org default.

## Trade-offs

| Approach | Pros | Cons |
|----------|------|------|
| **Per-task `archive_after` + server worker** (proposed) | Fine-grained control, no runner changes, fits existing archive semantics | New background worker; one more column to migrate |
| **Org-wide TTL only** | Simpler — one number to set | Can't keep individual tasks around longer; awkward for tasks a human is following |
| **Runner-side cleanup of unarchived tasks** | No server changes | Conflates "archived" with "cleaned up"; runner reaches into task lifecycle decisions it doesn't own; multiple runners would each need the same policy |
| **Auto-archive on terminal transition (immediately)** | Trivial implementation | Defeats the purpose of `archived = false` as a UI inbox; humans can't review failures before they vanish |
| **Hard-delete instead of archive** | Reclaims disk faster (logs, links, events go too) | Loses audit trail and breaks `parent` references from child tasks; archived rows are cheap to keep |

The proposed design keeps the existing archive semantics intact and only adds an automated trigger for the same operation a human does today. The runner doesn't change at all — its `Prune()` already does the right thing for archived tasks.

`interval` rather than `archive_at` timestamp means restarts and updates extend the deadline naturally (because `updated_at` advances), which matches the intent: "X time after the task is actually done."

## Open Questions

1. **Should child tasks inherit `archive_after` from their parent?** Right now a parent can pass it explicitly to `create_child_task`. Inheriting would mean a workflow only needs to set the timeout once at the top. The risk is that a long-lived parent task with `archive_after = NULL` would silently override a sensible org default for all its children — probably surprising. Likely answer: don't inherit, just rely on the org default.

2. **What about archived tasks with unarchived children?** A parent archived by the worker might still have running or terminal-but-unarchived children. The current archive operation doesn't cascade. Most likely we leave this alone (children get archived independently when their own timer fires), but should confirm there's no UI assumption that an archived parent implies archived descendants.

3. **Multi-replica server.** Today the server is a single process. If we ever run multiple replicas, two workers will both try to archive the same task. The transactional `GetTaskForUpdate` + `ShouldAutoArchive` re-check makes this correct (one wins, the other no-ops), but wastes work. Gating with `pg_try_advisory_lock` is cheap and could be added at that point.

4. **Org default scope.** Org-level default is proposed above. Should there also be a per-workspace default (workspaces.yaml or the server-managed workspace config from the [server-managed-workspaces proposal](./server-managed-workspaces.md))? Probably yes eventually — different workspaces have very different "how long do I care" profiles — but it can be a follow-up; the org default + per-task override covers the immediate need.

5. **Surfacing auto-archived tasks.** Once a task is archived it falls out of `ListTasks` (which filters `archived = FALSE`). Users investigating "why did my container disappear" need a way to find these — the existing unarchived task views already support this, but discoverability could be improved (e.g. a dedicated "Recently auto-archived" filter). Out of scope for this proposal but worth noting.
