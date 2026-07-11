# Include Archived Tasks in the Task List

Issue: https://github.com/icholy/xagent/issues/1334

## Problem

The task list excludes archived tasks entirely. Archiving is a soft-delete — the rows stay in the `tasks` table — but `archived = FALSE` is hard-coded into every listing surface:

- `ListTasksPage` (`internal/store/sql/queries/task.sql`) — `WHERE archived = FALSE`.
- The partial keyset index `tasks_org_created_id_idx` — `WHERE archived = FALSE`.
- The `ListTasks` RPC handler, the Web UI task list (`webui/src/routes/tasks.index.tsx`), the `list_tasks` MCP tool, and the `xagent task list` CLI — all consume that query.

Once a task is archived (manually or by the auto-archive loop) it disappears from the UI and every listing surface. A user who wants to revisit an old, archived investigation — or confirm that auto-archive fired — has no path back to it short of knowing the id and deep-linking to `/tasks/{id}`. This matters more over time: auto-archive (#633) is making archived tasks the *majority* of an org's history, all of it invisible.

We want an **optional** way to include archived tasks in the list, **off by default**, without regressing the normal (active-only) view or the keyset pagination that just landed for `ListTasks` (`proposals/implemented/task-pagination.md`). That proposal's Open Questions anticipated this: a "show archived" toggle becomes another request field that changes which rows the keyset scan visits, and the cursor's JSON blob was kept flexible so it can carry the filter.

## Design

### Overview

Add a single `include_archived` boolean to `ListTasksRequest`. When false (the default), the list behaves exactly as today: active tasks only, backed by the tight partial index. When true, the query drops the `archived = FALSE` predicate and returns active **and** archived tasks interleaved by `created_at DESC, id DESC`, backed by a new full index. The page token binds the filter so a cursor minted under one filter cannot be silently replayed under the other. The Web UI gains a "Show archived" switch beside the page-size selector; archived rows are marked with the existing `ArchivedBadge` and muted. The MCP tool and CLI each gain a matching opt-in flag.

The keyset itself does not change shape semantically — the ordering is still `created_at DESC, id DESC`, and archived rows simply take their place in that ordering. That is what makes this an additive change rather than a rework of the cursor.

### 1. Request field shape

`proto/xagent/v1/xagent.proto` — add one field to the existing request:

```protobuf
message ListTasksRequest {
  int32 page_size = 1;         // Max tasks to return (default: 50, max: 100)
  string page_token = 2;       // Opaque cursor from a previous next_page_token; empty for the first page
  bool include_archived = 3;   // Include archived tasks alongside active ones (default: false)
}
```

`include_archived = false` (the proto3 default, and what every existing empty-request caller sends) preserves today's behavior exactly. `true` means "active **and** archived," matching the mental model of a "Show archived" toggle — it *adds* archived rows to the active list rather than replacing it.

A bool rather than a filter enum is chosen deliberately; see [Trade-offs](#bool-include_archived-vs-a-filter-enum). The response (`ListTasksResponse`) is unchanged — the `Task.archived` field (already present, tag 13) is what lets clients distinguish rows.

### 2. SQL query

`internal/store/sql/queries/task.sql` — make the archived predicate conditional on a new arg:

```sql
-- name: ListTasksPage :many
SELECT id, name, runner, workspace, status, command, version, org_id, archived, created_at, updated_at, auto_archive, shell_session
FROM tasks
WHERE org_id = sqlc.arg(org_id)
  AND (sqlc.arg(include_archived)::bool OR archived = FALSE)
  AND (
    NOT sqlc.arg(use_cursor)::bool
    OR (created_at, id) < (sqlc.arg(cursor_created_at)::timestamp, sqlc.arg(cursor_id)::bigint)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);
```

When `include_archived` is true the `archived = FALSE` half of the disjunction drops out and the scan visits all of the org's rows in `created_at DESC, id DESC` order. The keyset predicate `(created_at, id) < (cursor)` is untouched and remains correct, because the ordering the cursor anchors to is identical in both modes — archived rows are just interleaved by timestamp. The over-fetch-and-trim and token building inside `pagination.List` are unaffected.

### 3. Pagination index

The existing index is **partial** — it only contains non-archived rows:

```sql
CREATE INDEX tasks_org_created_id_idx
  ON tasks (org_id, created_at DESC, id DESC)
  WHERE archived = FALSE;
```

The planner can only use a partial index for a query whose predicate implies the index's `WHERE`. The default (`include_archived = false`) path still carries `archived = FALSE`, so it keeps using this index — no regression on the hot path. But the `include_archived = true` path has no such predicate, so it *cannot* use the partial index and would fall back to a bitmap heap scan + sort of the org's whole history.

Add a **second, full index** that covers all rows in the same order:

```sql
-- migrate:up
CREATE INDEX tasks_org_created_id_all_idx
  ON tasks (org_id, created_at DESC, id DESC);

-- migrate:down
DROP INDEX tasks_org_created_id_all_idx;
```

Both indexes are kept:

- The default active-only scan (every 60s poll, every first page load) uses the **partial** index `tasks_org_created_id_idx`. Because auto-archive is pushing most rows into the archived state, this index stays small and dense — exactly the case it was introduced for — and the active-only scan never pays to skip archived index entries.
- The opt-in include-archived scan uses the new **full** index `tasks_org_created_id_all_idx`, so it too is an ordered index range scan with no sort, just over a larger set.

Keeping both trades a second index's write-amplification for keeping the common path on the tight partial index; see [Trade-offs](#two-indexes-vs-one-full-index) for why that's the right call given auto-archive.

### 4. Store layer

`internal/store/task.go` — thread the flag through `ListTasksPageParams`, the SQL args, and the cursor.

```go
type ListTasksPageParams struct {
	OrgID           int64
	PageSize        int32  // 0 means the default (50); max 100
	PageToken       string // opaque token from a previous page; empty for the first page
	IncludeArchived bool   // include archived tasks alongside active ones
}

// taskCursor is the keyset a task page token encodes. created_at is not
// unique, so id is the tiebreaker. IncludeArchived binds the token to the
// filter it was minted under so a cursor can't be replayed across filters.
type taskCursor struct {
	CreatedAt       time.Time `json:"c"`
	ID              int64     `json:"i"`
	IncludeArchived bool      `json:"a,omitempty"`
}

func (src taskSource) Query(ctx context.Context, cursor *taskCursor, limit int32) ([]*model.Task, error) {
	if cursor != nil && cursor.IncludeArchived != src.params.IncludeArchived {
		return nil, fmt.Errorf("%w: page token does not match include_archived", pagination.ErrInvalidRequest)
	}
	args := sqlc.ListTasksPageParams{
		OrgID:           src.params.OrgID,
		IncludeArchived: src.params.IncludeArchived,
		UseCursor:       cursor != nil,
		PageLimit:       limit,
	}
	if cursor != nil {
		args.CursorCreatedAt = cursor.CreatedAt
		args.CursorID = cursor.ID
	}
	rows, err := src.store.q(src.tx).ListTasksPage(ctx, args)
	if err != nil {
		return nil, err
	}
	return toModelTasks(rows)
}

func (src taskSource) Cursor(t *model.Task) taskCursor {
	return taskCursor{CreatedAt: t.CreatedAt, ID: t.ID, IncludeArchived: src.params.IncludeArchived}
}
```

Two things to note:

- **Token binds the filter.** `Cursor` stamps the request's `IncludeArchived` into every token it mints; `Query` rejects a cursor whose stamp disagrees with the current request, returning a wrapped `pagination.ErrInvalidRequest` (→ `CodeInvalidArgument`). This is entirely contained in the store's `taskSource`; the generic `internal/pagination` package is untouched — it already treats the cursor as an opaque `C` it only JSON-marshals. See [Trade-offs](#binding-the-filter-into-the-token-vs-leaving-it-free).
- `json:"a,omitempty"` keeps the common (`false`) token byte-compatible with tokens minted before this change, so in-flight tokens from the active-only path keep decoding.

### 5. Server handler

`internal/server/apiserver/task.go` — pure pass-through, one new line:

```go
page, err := s.store.ListTasksPage(ctx, nil, store.ListTasksPageParams{
	OrgID:           caller.OrgID,
	PageSize:        req.PageSize,
	PageToken:       req.PageToken,
	IncludeArchived: req.IncludeArchived,
})
```

The existing `errors.Is(err, pagination.ErrInvalidRequest)` → `CodeInvalidArgument` mapping now also covers the token/filter-mismatch case for free. No scope change: `include_archived` reads the same rows the caller can already read for their org; `OpTaskRead` still gates it.

### 6. Web UI

`webui/src/routes/tasks.index.tsx` — add a "Show archived" `Switch` (the component exists at `webui/src/components/ui/switch.tsx`) beside the page-size `Select`, persisted per-org like the page size:

```tsx
const [showArchived, setShowArchived] = useOrgLocalStorage('tasks-show-archived', 'false')
const includeArchived = showArchived === 'true'

const { data, isLoading, error, isPlaceholderData, refetch } = useQuery(
  listTasks,
  { pageSize: Number(pageSize), pageToken, includeArchived },
  { placeholderData: keepPreviousData, refetchInterval: 60000 },
)

// Flipping the toggle invalidates the cursor stack (the token is filter-bound),
// so restart from the first page — exactly how page-size changes already behave.
const handleShowArchivedChange = (checked: boolean) => {
  setShowArchived(checked ? 'true' : 'false')
  setTokens([])
}
```

`useOrgLocalStorage` stores strings and ignores empty writes, so `'true'`/`'false'` round-trip cleanly. Resetting `tokens` on toggle mirrors the existing `handlePageSizeChange` and means the UI never replays a filter-bound token under the wrong filter — the store's rejection is a backstop, not a path the UI exercises.

**Distinguishing archived rows.** The `ArchivedBadge` component (`webui/src/components/archived-badge.tsx`) already renders a muted "archived" badge when `task.archived` and `null` otherwise — reuse it. In `TaskRow`, render it next to the task name, and mute the row when archived:

```tsx
<TableRow className={task.archived ? 'text-muted-foreground' : undefined}>
  <TableCell>
    <Link ...>{task.name || `Unnamed - ${task.id}`}</Link>
    <ArchivedBadge task={task} />
  </TableCell>
  ...
```

The archive action column already gates on `canArchiveTask(task)`, which is false for archived tasks, so archived rows simply show no archive button — no extra handling needed. Run `pnpm lint` in `webui/` before finishing (CI enforces ESLint).

### 7. MCP tool and CLI

Both surfaces send an empty `ListTasksRequest` today and only ever see the first page (neither paginates). Adding the opt-in is cheap parity:

**MCP `list_tasks`** (`internal/server/mcpserver/mcpserver.go`) — give the input an optional field and surface `archived` in the summary so results are distinguishable:

```go
type listTasksInput struct {
	IncludeArchived bool `json:"include_archived,omitempty" jsonschema:"Include archived tasks (default: false)"`
}

// in listTasks:
resp, err := h.service.ListTasks(ctx, &xagentv1.ListTasksRequest{
	IncludeArchived: input.IncludeArchived,
})

// taskSummary gains:
type taskSummary struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Workspace string `json:"workspace"`
	Status    string `json:"status"`
	Archived  bool   `json:"archived,omitempty"`
	URL       string `json:"url,omitempty"`
}
```

**CLI `xagent task list`** (`internal/command/task_list.go`) — add a bool flag threaded into the request:

```go
&cli.BoolFlag{
	Name:  "include-archived",
	Usage: "include archived tasks",
},
// ...
resp, err := client.ListTasks(ctx, &xagentv1.ListTasksRequest{
	IncludeArchived: cmd.Bool("include-archived"),
})
```

Both default to false, so existing scripts and agent behavior are unchanged.

## Implementation Plan

1. **Proto field** — Delivers: `include_archived` on `ListTasksRequest` + regenerated Go/TS (`mise run generate`, webui buf generate). Depends on: nothing. Verifiable by: generated code compiles; field present in both stubs.
2. **Full index migration** — Delivers: `tasks_org_created_id_all_idx` migration. Depends on: nothing. Verifiable by: `dbmate up`/`down` run cleanly; `\d tasks` shows the new index.
3. **SQL query + store** — Delivers: conditional `archived` predicate in `ListTasksPage`, `IncludeArchived` on `ListTasksPageParams`/`taskCursor`, filter threading + token-mismatch rejection (`sqlc generate`). Depends on: (2). Verifiable by: store tests — active-only unchanged; include-archived returns archived rows in keyset order; a token minted under one filter is rejected under the other with `ErrInvalidRequest`.
4. **Server handler** — Delivers: pass `req.IncludeArchived` through. Depends on: (1), (3). Verifiable by: handler test paging with `include_archived` true/false; mismatch → `CodeInvalidArgument`.
5. **Web UI** — Delivers: "Show archived" switch (reset tokens on change), archived badge + muted row. Depends on: (4). Verifiable by: rendering against an org with archived tasks; toggling shows/hides them and resets to page 1; `pnpm lint` passes.
6. **MCP tool + CLI** — Delivers: `include_archived` on the `list_tasks` tool (+ `archived` in the summary) and a `--include-archived` CLI flag. Depends on: (4). Verifiable by: tool call / `xagent task list --include-archived` returns archived tasks; default omits them.

## Trade-offs

### `bool include_archived` vs a filter enum

**Chosen: a bool.** The feature the issue asks for is a binary toggle — "also show archived." A bool expresses exactly that, is the proto3 default-false so every existing caller is unaffected, and keeps the query a one-line conditional. A broader enum (e.g. `TaskFilter { ACTIVE, ARCHIVED, ALL }`, or a repeated status filter) would additionally allow "archived **only**," which no surface currently needs — the UI toggle, the CLI, and the MCP tool all want "active, optionally plus archived." An enum is also a larger, harder-to-narrow API commitment. If an "archived only" view or status filtering is wanted later, it can be added as a *separate* additive field (or the bool can be widened to an enum in a backward-compatible way) without redoing this work. Starting narrow avoids designing a filter language on speculation, matching how the pagination proposal deferred its `pg_trgm` search filter until there was demand.

### Two indexes vs one full index

**Chosen: keep the partial index and add a full one.** The alternative — replace the partial index with a single full `tasks_org_created_id_all_idx` and let the active-only path filter `archived = FALSE` during the scan — is simpler (one index, less write amplification) but degrades the *hot* path. With auto-archive (#633) driving most of an org's tasks into the archived state, an active-only page over a full index would have to read and discard a growing majority of archived index entries to fill each 50-row page — the exact skip-the-dead-rows cost the partial index was introduced to eliminate. The default poll runs constantly; the include-archived scan is a deliberate, occasional opt-in. So the common path keeps its tight partial index, and the rare path gets its own full index. The cost is one extra index maintained on writes; acceptable, and revisitable if write throughput ever becomes the bottleneck (in which case collapsing to the single full index is the fallback).

A composite `(org_id, archived, created_at DESC, id DESC)` index was also considered: it serves the active-only path well but *cannot* serve the include-archived path as a single ordered scan, because grouping by `archived` first breaks the global `created_at` ordering the keyset needs. It doesn't solve the opt-in path, so it isn't a substitute for the full index.

### Binding the filter into the token vs leaving it free

**Chosen: bind the filter into the token.** The keyset `(created_at, id)` is technically valid under either filter — the ordering is the same — so replaying a token across filters would not *corrupt* results; it would resume from that position over whichever row set the new filter selects. But it is a latent footgun: a caller that flips `include_archived` while paging would get a page that is internally consistent yet mismatched with what the toggle now claims to show, with no error. Because the pagination proposal deliberately kept the token an opaque JSON blob "flexible" for exactly this, stamping `IncludeArchived` into the cursor and rejecting a mismatched replay is nearly free (one field, one comparison in `taskSource.Query`) and turns a confusing silent-inconsistency into a clear `CodeInvalidArgument`. The `omitempty` tag keeps the default-false token wire-compatible with tokens already in flight.

The alternative — leave the token filter-agnostic and just document "reset pagination when you change the filter" — is simpler and would be fine for the UI alone (which already resets the token stack on toggle, as it does on page-size change). It was rejected because it pushes a correctness expectation onto every non-UI client (CLI scripts, future API consumers) that the server can cheaply enforce instead. Binding is defensive rather than strictly required; a reviewer who prefers the minimal change can drop the `taskCursor` field and the `Query` check without affecting the rest of the design.

### Surfacing on MCP/CLI vs UI-only

**Chosen: surface everywhere, default off.** The store change makes archived tasks reachable through the shared `ListTasksPage`; exposing the flag on the MCP tool and CLI is a few lines each and keeps the three read surfaces consistent (an orchestrator agent looking for a task it archived, or a script auditing archived work, both benefit). Adding `archived` to the MCP `taskSummary` is needed regardless so agents can tell the rows apart. The cost is negligible and every surface stays default-false, so nothing changes for callers that don't ask.

## Open Questions

1. **Archived-only view.** This proposal intentionally omits "archived **only**." If it turns out users want to browse *just* the archive (not active + archived interleaved), is that a third UI state (a tri-state select instead of a switch) plus widening the bool to an enum — or is "show archived + eyeball the badges" enough?
2. **Row styling depth.** Muting the row text + the existing `ArchivedBadge` is the proposed minimum. Is that sufficient contrast, or should archived rows also get a distinct background / be visually grouped below active ones? (Grouping fights the single `created_at DESC` keyset ordering, so interleaved-with-badge is the low-friction default.)
3. **Default page size when archived included.** With archived rows included, an org's list is much longer. The default page size (20 in the UI, 50 in the RPC) is unchanged here — do we want a different default when the toggle is on, or is prev/next paging enough?
