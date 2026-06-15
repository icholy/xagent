# Task Pagination

Issue: https://github.com/icholy/xagent/issues/1012

## Problem

The `ListTasks` RPC and the task list page (`webui/src/routes/tasks.index.tsx`) are unpaginated. The RPC request takes no parameters and the store query fetches every non-archived task for the org in one shot:

```sql
-- name: ListTasks :many
SELECT id, name, runner, workspace, status, command, version, org_id, archived, created_at, updated_at, auto_archive
FROM tasks
WHERE archived = FALSE AND org_id = $1
ORDER BY created_at DESC;
```

The web UI fetches the full list, refetches it every 60 seconds, and filters by name **client-side**. As an org accumulates tasks this becomes increasingly expensive: a growing payload on every poll, all rows rendered into a single table, and a query with no bound. The task pages and the `ListTasks` RPC should be paginated.

## Design

### Overview

Add **keyset (cursor) pagination** to `ListTasks`. The request gains a page size and an opaque page token; the response returns a `next_page_token`. The name search currently done client-side moves server-side so it works across the full dataset rather than only the loaded page. The web UI switches to `useInfiniteQuery` with a "Load more" control.

Keyset pagination (rather than `LIMIT`/`OFFSET`) is chosen because tasks are ordered `created_at DESC` and new tasks are continually inserted at the top while the UI polls. Offset pagination would skip or duplicate rows whenever a task is created between page loads. A cursor anchored to a specific row is stable against inserts. See [Trade-offs](#trade-offs).

### 1. Proto Definitions

`proto/xagent/v1/xagent.proto` — replace the empty request and extend the response:

```protobuf
message ListTasksRequest {
  int32 page_size = 1;     // Max tasks to return (default: 50, max: 100)
  string page_token = 2;   // Opaque cursor from a previous next_page_token; empty for the first page
  string filter = 3;       // Optional case-insensitive substring match on task name
}

message ListTasksResponse {
  repeated Task tasks = 1;
  string next_page_token = 2;  // Token for the next page; empty when there are no more results
}
```

The `page_token` is opaque to clients. The server encodes the keyset of the last returned row — its `created_at` and `id` — as a base64 string. Clients must treat it as a blob and pass it back verbatim, matching the convention used by Google AIP-158-style pagination.

### 2. Cursor Encoding

A small internal helper encodes/decodes the cursor. The keyset is `(created_at, id)` because `created_at` alone is not unique — two tasks can share a timestamp, so `id` is the tiebreaker.

```go
// internal/server/apiserver/task.go (or a small pagination helper)

type taskCursor struct {
    CreatedAt time.Time `json:"c"`
    ID        int64     `json:"i"`
}

func encodeTaskCursor(c taskCursor) (string, error) {
    b, err := json.Marshal(c)
    if err != nil {
        return "", err
    }
    return base64.URLEncoding.EncodeToString(b), nil
}

func decodeTaskCursor(token string) (taskCursor, error) {
    b, err := base64.URLEncoding.DecodeString(token)
    if err != nil {
        return taskCursor{}, err
    }
    var c taskCursor
    if err := json.Unmarshal(b, &c); err != nil {
        return taskCursor{}, err
    }
    return c, nil
}
```

An invalid/undecodable `page_token` is reported as `connect.CodeInvalidArgument`.

### 3. SQL Query

`internal/store/sql/queries/task.sql` — add a tiebreaker to the ordering, a keyset predicate, an optional name filter, and a limit. The existing `ListTasks` query is updated (callers that want every task continue to use the unfiltered path; see [Other callers](#5-other-callers)):

```sql
-- name: ListTasksPage :many
SELECT id, name, runner, workspace, status, command, version, org_id, archived, created_at, updated_at, auto_archive
FROM tasks
WHERE archived = FALSE
  AND org_id = sqlc.arg(org_id)
  AND (
    sqlc.arg(filter)::text = ''
    OR name ILIKE '%' || sqlc.arg(filter)::text || '%'
  )
  AND (
    NOT sqlc.arg(use_cursor)::bool
    OR (created_at, id) < (sqlc.arg(cursor_created_at)::timestamptz, sqlc.arg(cursor_id)::bigint)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);
```

Notes:

- `ORDER BY created_at DESC, id DESC` makes the sort deterministic. The current query orders by `created_at DESC` only; the `id` tiebreaker is required for a correct keyset.
- The `use_cursor` boolean lets the first page (no token) skip the keyset predicate cleanly within a single query.
- The server fetches `page_size + 1` rows. If `page_size + 1` rows come back, there is a next page: the extra row is dropped from the response and the `next_page_token` is built from the keyset of the **last returned** row. Otherwise `next_page_token` is empty.
- A partial index supports the keyset scan:

```sql
CREATE INDEX tasks_org_created_id_idx
  ON tasks (org_id, created_at DESC, id DESC)
  WHERE archived = FALSE;
```

### 4. Store Layer

`internal/store/task.go` — add a paged variant alongside the existing `ListTasks`:

```go
type ListTasksPageParams struct {
    OrgID           int64
    Filter          string
    Cursor          *taskCursor // nil for the first page
    Limit           int32       // server passes pageSize + 1
}

func (s *Store) ListTasksPage(ctx context.Context, tx *sql.Tx, p ListTasksPageParams) ([]*model.Task, error) {
    args := sqlc.ListTasksPageParams{
        OrgID:     p.OrgID,
        Filter:    p.Filter,
        UseCursor: p.Cursor != nil,
        PageLimit: p.Limit,
    }
    if p.Cursor != nil {
        args.CursorCreatedAt = p.Cursor.CreatedAt
        args.CursorID = p.Cursor.ID
    }
    rows, err := s.q(tx).ListTasksPage(ctx, args)
    if err != nil {
        return nil, err
    }
    return toModelTasks(rows)
}
```

The existing `ListTasks(ctx, tx, orgID)` method and its SQL query are retained for internal callers that legitimately need every task (see below).

### 5. Other callers

`ListTasks` (store) is also used outside the web-facing RPC. These callers must keep fetching the full set and are **not** migrated to the paged query:

- `ListRunnerTasks` / runner reconciliation — needs all tasks for a runner, not a page.
- Any background reconciliation or cleanup loops.

Audit of `store.ListTasks` callers is part of implementation; the rule is: only the `XAgentService.ListTasks` RPC handler switches to `ListTasksPage`.

### 6. Server Handler

`internal/server/apiserver/task.go` — follow the validation shape already used by `ListExternalEvents` (default + max, `CodeInvalidArgument` on out-of-range):

```go
func (s *Server) ListTasks(ctx context.Context, req *xagentv1.ListTasksRequest) (*xagentv1.ListTasksResponse, error) {
    caller := apiauth.MustCaller(ctx)
    if !caller.Scopes.Allow(authscope.OpTaskRead) {
        return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot list tasks"))
    }

    const (
        defaultPageSize = 50
        maxPageSize     = 100
    )
    pageSize := cmp.Or(int(req.PageSize), defaultPageSize)
    if pageSize < 1 || pageSize > maxPageSize {
        return nil, connect.NewError(connect.CodeInvalidArgument,
            fmt.Errorf("page_size must be between 1 and %d", maxPageSize))
    }

    var cursor *taskCursor
    if req.PageToken != "" {
        c, err := decodeTaskCursor(req.PageToken)
        if err != nil {
            return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid page_token: %w", err))
        }
        cursor = &c
    }

    tasks, err := s.store.ListTasksPage(ctx, nil, store.ListTasksPageParams{
        OrgID:  caller.OrgID,
        Filter: req.Filter,
        Cursor: cursor,
        Limit:  int32(pageSize + 1), // fetch one extra to detect a next page
    })
    if err != nil {
        return nil, connect.NewError(connect.CodeInternal, err)
    }

    var nextToken string
    if len(tasks) > pageSize {
        last := tasks[pageSize-1]
        tasks = tasks[:pageSize]
        nextToken, err = encodeTaskCursor(taskCursor{CreatedAt: last.CreatedAt, ID: last.ID})
        if err != nil {
            return nil, connect.NewError(connect.CodeInternal, err)
        }
    }

    resp := &xagentv1.ListTasksResponse{
        Tasks:         make([]*xagentv1.Task, len(tasks)),
        NextPageToken: nextToken,
    }
    for i, t := range tasks {
        resp.Tasks[i] = t.Proto(s.baseURL)
    }
    return resp, nil
}
```

### 7. Web UI

`webui/src/routes/tasks.index.tsx` switches from `useQuery` to `useInfiniteQuery` (connect-query supports it via `useInfiniteQuery` with `pageParamKey: 'pageToken'` and `getNextPageParam`).

```tsx
const {
  data,
  isLoading,
  error,
  fetchNextPage,
  hasNextPage,
  isFetchingNextPage,
  refetch,
} = useInfiniteQuery(
  listTasks,
  { pageSize: 50, filter: debouncedSearch },
  {
    pageParamKey: 'pageToken',
    getNextPageParam: (lastPage) => lastPage.nextPageToken || undefined,
    refetchInterval: 60000,
  },
)

const tasks = data?.pages.flatMap((p) => p.tasks) ?? []
```

Changes:

- **Search moves server-side.** The current client-side `allTasks.filter(...)` is removed. The search input is debounced (~300ms) and passed as the `filter` request field, so a new query (a fresh first page) is issued when the term changes. This makes search correct across all tasks instead of only the currently loaded page.
- **"Load more" control.** A button below the table calls `fetchNextPage()` and is shown while `hasNextPage` is true, disabled while `isFetchingNextPage`. (Infinite scroll via an intersection observer is a possible follow-up; a button keeps the first cut simple.)
- **Polling.** `refetchInterval: 60000` with `useInfiniteQuery` refetches all currently loaded pages, preserving the live-update behavior. Because pagination is keyset-based, refetched pages remain stable even as new tasks are inserted at the top — newly created tasks appear when the first page is refetched.

No other RPC clients consume `ListTasksResponse` in a way that breaks: adding `next_page_token` is backward compatible, and existing callers that send an empty request get `page_size = 0 → default 50`.

### 8. Implementation Order

1. Proto changes + regenerate (`mise run generate` for Go, `pnpm` buf generate for webui).
2. SQL migration: add `tasks_org_created_id_idx`; add the `ListTasksPage` query; `sqlc` generate.
3. Store layer: `ListTasksPage` + params struct.
4. Server handler: cursor encode/decode, validation, next-page-token assembly.
5. Web UI: `useInfiniteQuery`, debounced server-side filter, "Load more".
6. Tests (store-level keyset correctness; handler validation and token round-trip).

## Trade-offs

### Keyset (cursor) vs offset/limit

**Chosen: keyset.** Tasks are ordered newest-first and inserted continuously while the UI polls every 60s. With `OFFSET`, inserting a task at the top shifts every subsequent row down by one, so page 2 would repeat the last item of page 1 (or skip one). Keyset anchors each page to a concrete `(created_at, id)` and is immune to inserts above the cursor. The cost is that random page access ("jump to page 7") is not supported — acceptable for a task list where users scan recent work and "Load more" / search are the primary navigation modes.

### Server-side vs client-side search

**Chosen: server-side `filter`.** Once results are paginated, the existing client-side `.filter()` would only match within already-loaded pages, silently hiding matches further down. Moving the substring match into the query (`name ILIKE '%...%'`) keeps search correct over the entire dataset. The trade-off is an un-indexed `ILIKE` scan; at current task volumes this is fine, and a trigram index (`pg_trgm`) is a clear future optimization if needed.

### Simple `limit` (the `ListExternalEvents` convention) vs full pagination

`ListExternalEvents` uses a bare `limit` with no cursor — it only ever exposes the latest N events and has no "next page". That is insufficient here: the issue asks for the task **pages** to be paginated, i.e. the ability to page through the entire history, not just cap the first slice. Hence the richer `page_token`/`next_page_token` contract. The validation shape (default + max + `CodeInvalidArgument`) still mirrors the existing convention.

### "Load more" vs numbered pages vs infinite scroll

**Chosen: "Load more" button.** Numbered pages require total counts and offset access, which keyset pagination doesn't provide cheaply. A "Load more" button is the simplest control that fits cursor semantics; infinite-scroll-on-view can layer on later without an API change.

## Open Questions

1. **Default page size.** Proposed 50 (max 100). Is 50 the right first-screen size for the table, or should it match a typical viewport more tightly (e.g. 25)?
2. **Archived tasks.** The list excludes archived tasks today. Out of scope here, but if a future "show archived" toggle is added, it would become another request field that participates in the cursor's filter — worth keeping the cursor format flexible (the JSON blob already is).
3. **Total count.** Should the response include an approximate total (e.g. for a "showing X of ~Y" label)? An exact `COUNT(*)` defeats some of the pagination win; an estimate via `pg_class.reltuples` is possible but org-filtered counts complicate it. Left out unless the UI needs it.
4. **Status filtering.** Should server-side filtering extend beyond name to status (running/finished/etc.)? The UI only filters by name today; adding status filters is a natural follow-up that fits the same `filter`-style request fields.
