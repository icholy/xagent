# Task Event Pagination

Issue: https://github.com/icholy/xagent/issues/1325

## Problem

A task's event stream is returned unbounded. Two RPCs hand events back with no limit:

- **`ListEventsByTask`** returns the full task timeline — every arm of the event union
  (instruction, external, report, lifecycle, link) in chronological order. Its store query
  is `ORDER BY id` with no `LIMIT`:

  ```sql
  -- name: ListEventsByTask :many
  SELECT id, org_id, created_at, task_id, type, wake, payload
  FROM events
  WHERE task_id = $1 AND org_id = $2
    AND (cardinality(sqlc.arg(types)::text[]) = 0 OR type = ANY(sqlc.arg(types)::text[]))
  ORDER BY id;
  ```

  The web UI timeline (`webui/src/routes/tasks.$id.tsx`) fetches the whole list and refetches
  it — on a 60s interval and on every SSE `task_logs` signal — replacing the entire timeline
  each time.

- **`GetTaskDetails`** returns the agent **brief**: the same query filtered to
  `[instruction, external]` (`internal/server/apiserver/task.go`). It is consumed **only** by
  the agent's `get_my_task` tool (`internal/agentmcp/xmcp.go`), which projects the instruction
  events into an `instructions` list and dumps all brief events into an `events` list. The web
  UI calls `GetTaskDetails` for the task and links but **ignores its `events`** — the timeline
  comes from `ListEventsByTask`.

A long-lived task accumulates events continuously: every report, lifecycle transition, tool
call, and inbound PR/Jira event is a row. As the timeline grows, the `ListEventsByTask` payload
and the work to render it grow without bound.

This proposal targets that timeline. The **agent brief** (`GetTaskDetails`) is explicitly **out
of scope** — it stays unbounded and unchanged, per maintainer direction: it is already narrowed
to the two low-volume arms the agent needs (instruction + external), and it is the agent's
one-shot task picture rather than a scrollable surface (see
[The agent brief](#4b-the-agent-brief-gettaskdetails---unchanged-out-of-scope)).

Task **list** pagination already landed — the generic `internal/pagination` keyset package and
`Store.ListTasksPage` / `taskSource` (see `proposals/draft/task-pagination.md`). This proposal
reuses those conventions for the per-task event stream, adapting them for an append-only,
live-followed **timeline**.

## Design

### Overview

Paginate the **timeline** RPC, `ListEventsByTask`, with a **bidirectional keyset cursor** that
starts at the tail. The request keeps a single opaque `page_token`; the response returns **two**
tokens, `prev_page_token` (older) and `next_page_token` (newer).

The shape is dictated by how a timeline is used: open at the newest events, scroll **up** for
history, and watch new events arrive at the **bottom**. So:

- **Open = the tail.** An empty request token returns the **newest** page (one request, O(1)),
  not the oldest — the user sees current activity immediately with no history walk.
- **Scroll up = older.** `prev_page_token` (derived from the page's **first**/oldest row) fetches
  the previous, older page. It is **empty once history is exhausted** (the oldest event is
  loaded).
- **Live-follow = newer.** `next_page_token` (derived from the page's **last**/newest row) fetches
  newer events. It is **always populated** in the paged path: even at the tail with nothing newer
  yet, it returns the resume cursor so a client keeps polling forward for appends. The forward
  walk *is* the live-update mechanism — there is no separate live channel for the timeline.

**Every page is ascending (display order)**, in both directions and in the legacy path. The
cursor is a single boundary `id`; forward reads `id > X ORDER BY id ASC`, backward reads
`id < X ORDER BY id DESC` and the store reverses those rows to ascending before returning. The
direction is encoded *inside* the opaque token, so the request surface stays a single
`page_token`.

Because the event stream is **append-only** (rows are only ever inserted, with monotonically
increasing `id`), every fully-loaded page is an **immutable window** over a fixed id range.
That removes all of the machinery an in-place-mutating list needs: **no page reversal in the
client, no `refetchInterval`, no cache invalidation of loaded pages, and no head-aware
bookkeeping.** The web UI opens at the tail, prepends older pages on scroll-up, and appends the
live delta on each SSE `task_logs` signal.

The **agent brief** (`GetTaskDetails`) is deliberately **left unbounded** — it keeps fetching
all instruction + external events exactly as it does today. It is out of scope for this
proposal. See [The agent brief](#4b-the-agent-brief-gettaskdetails---unchanged-out-of-scope).

Pagination mechanics stay owned by the **store** and the `internal/pagination` package: the
store's `ListEventsByTaskPage` takes the request's `page_size`/`page_token` as plain values and
returns items plus the two tokens; the cursor type, direction encoding, token format, page-size
bounds, and over-fetch all live behind that boundary. The handler passes fields through and maps
errors.

Keyset (not `LIMIT`/`OFFSET`) is the right fit and is even simpler here than for tasks:
`events.id` is a monotonic, unique `bigserial`, so the cursor is a **single column** (`id`), with
no `(created_at, id)` tiebreaker. The existing `idx_events_task_id_id (task_id, id)` index serves
both the forward and backward range scans, so **no new migration is required**.

### 1. Proto Definitions

`proto/xagent/v1/xagent.proto` — extend the existing request/response. `task_id` stays field 1;
the pagination fields are additive, so existing callers are unaffected (see
[Backward compatibility](#7-backward-compatibility)):

```protobuf
message ListEventsByTaskRequest {
  int64 task_id = 1;
  int32 page_size = 2;    // Max events per page (default 50, max 200). 0 with an empty
                          // page_token selects the legacy unpaged path (all events, ascending).
  string page_token = 3;  // Opaque bidirectional cursor (encodes a boundary id + direction).
                          // Empty returns the newest page.
}

message ListEventsByTaskResponse {
  repeated Event events = 1;   // Always oldest-first (ascending id), every page and path.
  string prev_page_token = 2;  // Older page (scroll back); empty when history is exhausted.
  string next_page_token = 3;  // Newer page (scroll forward / live-follow); ALWAYS populated in
                               // the paged path so a client can keep polling the tail for appends.
                               // A page shorter than page_size means the tail is currently reached.
}
```

Both tokens are opaque — the server encodes a boundary `id` and a direction as base64. Clients
treat them as blobs and pass one back as the next request's `page_token`.

### 2. Pagination Package — one `Source`, two entry points

There is a **single** `Source` interface with **two query methods**, `QueryForward` and
`QueryBackward` (rather than one `Query` taking a direction flag), and two entry points that drive
it: the existing `List` (one-directional, single next-token — the task list) and a new `ListBi`
(bidirectional, two tokens — the timeline). Terminology is generic **forward/backward** by key
order, not domain newer/older: `QueryForward` reads the ascending side (keys above the cursor);
`QueryBackward` reads the descending side (keys below the cursor; a nil cursor starts at the
highest key — the newest page). A source implements only the directions it supports and returns
`ErrUnsupportedDirection` from the rest — the task list pages only backward (through its
newest-first rows) and errors from `QueryForward`; the event source supports both. `Config`,
`Page`, and `ErrInvalidRequest` are unchanged; we add `ErrUnsupportedDirection`, `BiPage`, and
`ListBi`. `Source` grows from one `Query` method to `QueryForward`/`QueryBackward`, so the
just-landed `taskSource` and `List` are adjusted (task keeps its query as `QueryBackward`, adds an
erroring `QueryForward`; `List` calls `QueryBackward`) — a mechanical, behavior-preserving refactor
of the task pagination code.

The storage-agnostic package deals in plain `int` for counts (`limit`, `Config.Default/Max`,
the internal `size`). `int32` appears at exactly two type boundaries and nowhere else: the
`page_size` request value, which is the proto field type passed verbatim into the entry points and
store params; and the `LIMIT` argument at the sqlc call, converted with `int32(limit)` inside each
store `Query*` method. (The just-landed `pagination.List`/`Source` use `int32` for `limit` too;
this narrows it to `int` as part of the same `Source` change.)

```go
// ErrUnsupportedDirection is returned by a Source asked to page in a direction it
// does not implement (e.g. the task list, backward-only, from QueryForward). List
// only ever calls QueryBackward, so handlers treat it as internal, not a bad request.
var ErrUnsupportedDirection = errors.New("unsupported page direction")

// Source walks a keyset in two directions and maps a row to a cursor.
// QueryBackward reads the descending side (keys below the cursor; a nil cursor
// starts at the highest key — the newest page). QueryForward reads the ascending
// side (keys above the cursor). Rows come back in the query's natural order
// (descending / ascending respectively); ListBi reverses backward pages so its Items
// are ascending. A one-directional list implements one method and returns
// ErrUnsupportedDirection from the other.
type Source[T, C any] interface {
    QueryBackward(ctx context.Context, cursor *C, limit int) ([]T, error)
    QueryForward(ctx context.Context, cursor *C, limit int) ([]T, error)
    Cursor(row T) C
}

// direction is which method a page token resumes from. It is an unexported binary,
// internal to token encoding — it is not a source-facing value. "No cursor" is a
// separate fact (nil cursor / empty token); the newest page is a backward read with
// no cursor.
type direction int8

const (
    backward direction = iota // QueryBackward (descending); nil cursor → newest page
    forward                   // QueryForward (ascending)
)

// biToken is what a bidirectional page token encodes: a caller cursor plus the
// direction to resume from. A token always carries backward (prev) or forward (next).
type biToken[C any] struct {
    Cursor C         `json:"c"`
    Dir    direction `json:"d"`
}

// BiPage is one page of a bidirectional keyset list. Items are always ascending
// (display order). PrevToken pages backward (older); NextToken pages forward (newer).
type BiPage[T any] struct {
    Items     []T
    PrevToken string // backward page; empty when no older rows remain
    NextToken string // forward page; append-only follow keeps this populated past the tail
}

// ListBi runs one page of a bidirectional keyset query. An empty pageToken reads the
// newest page (backward, no cursor). It over-fetches size+1 to detect more rows in
// the scanned direction, normalizes Items to ascending, and derives both tokens:
//   - PrevToken (backward): from the first row, when older rows remain.
//   - NextToken (forward): from the last row, always — the stream is append-only and
//     followed, so the tail is only "currently" the end. An empty forward poll
//     (no new rows yet) echoes the request token so the caller keeps its place.
func ListBi[T, C any](ctx context.Context, cfg Config, pageSize int32, pageToken string, src Source[T, C]) (*BiPage[T], error) {
    size := cmp.Or(int(pageSize), cfg.Default)
    if size < 1 || size > cfg.Max {
        return nil, fmt.Errorf("%w: page_size must be between 1 and %d", ErrInvalidRequest, cfg.Max)
    }
    dir := backward // no token → newest page (backward, no cursor)
    var cursor *C
    if pageToken != "" {
        t, err := decode[biToken[C]](pageToken)
        if err != nil {
            return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
        }
        cursor, dir = &t.Cursor, t.Dir
    }
    var rows []T
    var err error
    if dir == forward {
        rows, err = src.QueryForward(ctx, cursor, size+1) // ascending
    } else {
        rows, err = src.QueryBackward(ctx, cursor, size+1) // descending
    }
    if err != nil {
        return nil, err
    }
    more := len(rows) > size
    if more {
        rows = rows[:size]
    }
    if dir == backward {
        slices.Reverse(rows) // backward is fetched descending → ascending
    }
    page := &BiPage[T]{Items: rows}
    if len(rows) == 0 {
        if dir == forward {
            page.NextToken = pageToken // empty forward poll → keep the caller's place
        }
        return page, nil // empty newest page → empty stream; client re-requests newest
    }
    // Older rows remain when scrolling forward (by construction) or when the
    // backward scan over-fetched.
    if dir == forward || more {
        if page.PrevToken, err = encode(biToken[C]{Cursor: src.Cursor(rows[0]), Dir: backward}); err != nil {
            return nil, err
        }
    }
    // Forward token is always populated: append-only live-follow.
    if page.NextToken, err = encode(biToken[C]{Cursor: src.Cursor(rows[len(rows)-1]), Dir: forward}); err != nil {
        return nil, err
    }
    return page, nil
}
```

`ListBi` bakes the two timeline-specific rules — always-on `NextToken` and empty-poll echo — so
the store method is a thin call. (The lone case `ListBi` can't synthesize is an *empty* stream:
the newest page (backward, nil cursor) with zero rows returns empty tokens. A task effectively
always has its opening instruction event, and even if not, the web UI simply re-requests the
newest page on the next signal until the first event lands — no synthetic "zero cursor" is
needed.)

The existing `List` calls `src.QueryBackward(ctx, cursor, size+1)`, and the just-landed
`taskSource` splits into its existing backward query plus an erroring forward one:

```go
func (src taskSource) QueryBackward(ctx context.Context, cursor *taskCursor, limit int) ([]*model.Task, error) {
    // ... existing ListTasksPage query; convert limit to int32 only at the sqlc call ...
}

func (src taskSource) QueryForward(ctx context.Context, _ *taskCursor, _ int) ([]*model.Task, error) {
    return nil, fmt.Errorf("task list: %w", pagination.ErrUnsupportedDirection)
}
```

`List` only ever calls `QueryBackward`, so the erroring `QueryForward` never runs in practice; it
documents and enforces that the task list is one-directional.

### 3. Store Layer

#### SQL queries

`internal/store/sql/queries/event.sql` — add two paged variants alongside the retained
`ListEventsByTask`, one per scan direction. Both keep the optional `types` filter (parity with
the existing query, so future arm-filtered paging is a param, not a new query) and both are
covered by `idx_events_task_id_id`:

```sql
-- name: ListEventsByTaskBackward :many
-- Newest-first slice for the newest-page (no cursor) and scroll-back (id < cursor)
-- cases. Rows come back DESC; the caller reverses to ascending.
SELECT id, org_id, created_at, task_id, type, wake, payload
FROM events
WHERE task_id = sqlc.arg(task_id)
  AND org_id = sqlc.arg(org_id)
  AND (cardinality(sqlc.arg(types)::text[]) = 0 OR type = ANY(sqlc.arg(types)::text[]))
  AND (NOT sqlc.arg(use_cursor)::bool OR id < sqlc.arg(cursor_id)::bigint)
ORDER BY id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListEventsByTaskForward :many
-- Scroll-forward / live-follow slice (id > cursor), ascending.
SELECT id, org_id, created_at, task_id, type, wake, payload
FROM events
WHERE task_id = sqlc.arg(task_id)
  AND org_id = sqlc.arg(org_id)
  AND (cardinality(sqlc.arg(types)::text[]) = 0 OR type = ANY(sqlc.arg(types)::text[]))
  AND id > sqlc.arg(cursor_id)::bigint
ORDER BY id ASC
LIMIT sqlc.arg(page_limit);
```

Notes:

- **Single-column keyset.** `id` is a unique monotonic `bigserial` (the `events_id_seq` PK), so
  `id <=> cursor_id` is a total order — no `created_at` tiebreaker, unlike tasks. `id` order *is*
  insertion (stream) order.
- **No new index.** `idx_events_task_id_id ON events (task_id, id)` already exists and a B-tree
  serves both `id > ? ORDER BY id ASC` and `id < ? ORDER BY id DESC`.
- The `Backward` query serves both the newest page (`use_cursor = false`) and scroll-back
  (`use_cursor = true`); `ListBi` reverses its rows to ascending.
- The existing `ListEventsByTask` query is **retained** for the legacy unpaged path and internal
  callers (see [Backward compatibility](#7-backward-compatibility)).

#### Store method

`internal/store/event.go` — `eventCursor` and `eventSource` are unexported; callers only ever see
opaque tokens. `eventSource` implements `pagination.Source` with both query methods (unlike
`taskSource`, whose `QueryForward` errors):

```go
// eventCursor is the keyset a bidirectional event page token encodes. events.id is
// a unique monotonic bigserial, so it is a total order on its own — no tiebreaker.
type eventCursor struct {
    ID int64 `json:"i"`
}

// Timelines are dense (a report/lifecycle/tool row per step), so the default and
// max pages are larger than the task list's.
var listEventsPaging = pagination.Config{Default: 50, Max: 200}

type ListEventsByTaskPageParams struct {
    TaskID    int64
    OrgID     int64
    Types     []string // nil/empty → all arms; a future arm-filtered page passes e.g. [external]
    PageSize  int32    // 0 → default (50); max 200
    PageToken string   // opaque bidirectional cursor; empty for the newest page
}

// eventSource implements pagination.Source for a task's events, supporting both
// directions: QueryBackward → the Backward SQL, QueryForward → the Forward SQL.
type eventSource struct {
    store  *Store
    tx     *sql.Tx
    params ListEventsByTaskPageParams
}

func (src eventSource) types() []string {
    if src.params.Types == nil {
        return []string{} // nil encodes as SQL NULL; empty array matches the cardinality(...) = 0 guard
    }
    return src.params.Types
}

// QueryBackward reads descending; a nil cursor is the newest page (no lower bound).
func (src eventSource) QueryBackward(ctx context.Context, cursor *eventCursor, limit int) ([]*model.Event, error) {
    args := sqlc.ListEventsByTaskBackwardParams{
        TaskID: src.params.TaskID, OrgID: src.params.OrgID, Types: src.types(),
        UseCursor: cursor != nil, PageLimit: int32(limit), // int32 only at the sqlc boundary
    }
    if cursor != nil {
        args.CursorID = cursor.ID
    }
    rows, err := src.store.q(src.tx).ListEventsByTaskBackward(ctx, args)
    if err != nil {
        return nil, err
    }
    return toModelEvents(rows)
}

// QueryForward reads ascending, rows after the cursor.
func (src eventSource) QueryForward(ctx context.Context, cursor *eventCursor, limit int) ([]*model.Event, error) {
    rows, err := src.store.q(src.tx).ListEventsByTaskForward(ctx, sqlc.ListEventsByTaskForwardParams{
        TaskID: src.params.TaskID, OrgID: src.params.OrgID, Types: src.types(),
        CursorID: cursor.ID, PageLimit: int32(limit), // int32 only at the sqlc boundary
    })
    if err != nil {
        return nil, err
    }
    return toModelEvents(rows)
}

func (src eventSource) Cursor(e *model.Event) eventCursor {
    return eventCursor{ID: e.ID}
}

func (s *Store) ListEventsByTaskPage(ctx context.Context, tx *sql.Tx, p ListEventsByTaskPageParams) (*pagination.BiPage[*model.Event], error) {
    return pagination.ListBi(ctx, listEventsPaging, p.PageSize, p.PageToken, eventSource{store: s, tx: tx, params: p})
}
```

A bad `PageSize` or undecodable `PageToken` surfaces as a wrapped `pagination.ErrInvalidRequest`;
query failures surface as-is. Same contract as `ListTasksPage`.

### 4. Server Handler (`ListEventsByTask`)

`internal/server/apiserver/event.go` — keep the scope checks, then branch on whether the caller
opted into pagination. `page_size == 0 && page_token == ""` preserves today's behavior (all
events, ascending, no tokens); otherwise serve a bidirectional page.

```go
func (s *Server) ListEventsByTask(ctx context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
    caller := apiauth.MustCaller(ctx)
    // ... unchanged scope / instance checks ...

    // Legacy unpaged path: no pagination fields → all events, ascending.
    if req.PageSize == 0 && req.PageToken == "" {
        events, err := s.store.ListEventsByTask(ctx, nil, req.TaskId, caller.OrgID, nil)
        if err != nil {
            return nil, connect.NewError(connect.CodeInternal, err)
        }
        return &xagentv1.ListEventsByTaskResponse{Events: model.ProtoMap(events)}, nil
    }

    // Paged path: bidirectional keyset page (empty token → newest page).
    page, err := s.store.ListEventsByTaskPage(ctx, nil, store.ListEventsByTaskPageParams{
        TaskID:    req.TaskId,
        OrgID:     caller.OrgID,
        PageSize:  req.PageSize,
        PageToken: req.PageToken,
    })
    if err != nil {
        code := connect.CodeInternal
        if errors.Is(err, pagination.ErrInvalidRequest) {
            code = connect.CodeInvalidArgument
        }
        return nil, connect.NewError(code, err)
    }
    return &xagentv1.ListEventsByTaskResponse{
        Events:        model.ProtoMap(page.Items),
        PrevPageToken: page.PrevToken,
        NextPageToken: page.NextToken,
    }, nil
}
```

Every path returns events ascending, so the client never reorders. The `errors.Is` mapping is the
one place the handler acknowledges pagination, matching `ListTasks`.

### 4b. The agent brief (`GetTaskDetails`) — unchanged, out of scope

`GetTaskDetails` and the agent's `get_my_task` tool are **left exactly as they are**: they keep
fetching the full brief (all instruction + external events, ascending) with no bound, no tail,
and no paging.

This is a deliberate scope decision. The brief is the agent's one-shot picture of its task, read
once per wake, and it is already narrowed to the two low-volume, semantically-required arms
(instruction + external) — it excludes the high-volume arms (report, lifecycle, link) that make
the *timeline* grow. Paging it would force the agent to make follow-up calls just to reconstruct
its own instructions, and any tail risks dropping context the agent needs mid-task. The
unbounded growth this proposal targets is the full **timeline** (`ListEventsByTask`), not the
brief.

Concretely: `internal/server/apiserver/task.go`'s `GetTaskDetails` and
`internal/agentmcp/xmcp.go`'s `getMyTask` / `taskDetailsToMap` are untouched. If the brief ever
does become a problem for a pathological task, bounding it is a separate follow-up (a dedicated
paged agent tool, or a head-preserving tail) — not part of this change.

### 5. Web UI

`webui/src/routes/tasks.$id.tsx` switches the timeline from `useQuery(listEventsByTask)` to a
**bidirectional** `useInfiniteQuery`: open at the tail, prepend older pages on scroll-up, append
the live delta on SSE. Every page is already ascending, so pages flatten directly — **no
reversal**:

```tsx
const PAGE_SIZE = 50

const {
  data,
  fetchPreviousPage,     // older history (scroll up)
  hasPreviousPage,
  isFetchingPreviousPage,
  fetchNextPage,         // newer events (live-follow)
} = useInfiniteQuery(
  listEventsByTask,
  { taskId, pageSize: PAGE_SIZE },
  {
    // Empty initial pageParam → the newest (tail) page: one request on open.
    pageParamKey: 'pageToken',
    getPreviousPageParam: (firstPage) => firstPage.prevPageToken || undefined,
    // next_page_token is always set (it doubles as the live-follow cursor), so it
    // can't signal "stop"; we only fetch it on an SSE signal, never as "load more".
    getNextPageParam: (lastPage) => lastPage.nextPageToken || undefined,
  },
)

// TanStack keeps pages in navigation order and prepends fetchPreviousPage results,
// so flattening yields one ascending stream across all loaded pages.
const events = data?.pages.flatMap((p) => p.events) ?? []
const timeline = eventsToTimeline(events)
```

- **Open (O(1)).** The initial request carries an empty token and gets the newest page — the user
  sees current activity immediately, no history walk.
- **Scroll up.** A "Load older" control (or a scroll/intersection trigger) calls
  `fetchPreviousPage()` while `hasPreviousPage`; TanStack prepends the older page and
  `getPreviousPageParam` reads `prev_page_token`, which goes empty at the oldest event. The
  connect-query wrapper passes `getPreviousPageParam` through (verified against the installed
  types).
- **Live-follow.** On each SSE `task_logs` signal for this task, call `fetchNextPage()` once: it
  requests `id > <newest seen>` and appends any newly-inserted events at the bottom. Loaded full
  pages are immutable windows over append-only rows, so nothing already rendered changes — **no
  invalidation, no `refetchInterval`, no reversal, no head-aware bookkeeping.**
  `use-org-sse.ts`'s `task_logs` case changes from `invalidateQueries(listEventsByTask)` to
  "`fetchNextPage()` for this task's timeline."
- **Empty tail polls.** When nothing new has been appended, `fetchNextPage()` returns an empty
  page whose token echoes the same cursor; left alone these accumulate. Trim trailing empty pages
  after each follow fetch (the preceding page's `next_page_token` already points at the same
  resume cursor, so the forward walk is unaffected):

  ```tsx
  queryClient.setQueryData(key, (prev) =>
    !prev ? prev : { ...prev, pages: dropTrailingEmpty(prev.pages), pageParams: /* sliced to match */ },
  )
  ```

`GetTaskDetails` (task + links) keeps its existing `useQuery` + SSE invalidation unchanged; only
the timeline query moves to the bidirectional infinite model.

### 6. Other callers

`store.ListEventsByTask` (unpaged) is kept and still used by:

- `GetTaskDetails` — for the complete agent brief, unchanged (see
  [The agent brief](#4b-the-agent-brief-gettaskdetails---unchanged-out-of-scope)).
- Tests and any internal consumers that legitimately want the full stream.

Only the web UI timeline moves to the paged path. The `ListEventsByTask` **RPC** serves both:
legacy callers (empty page fields) get the full list; the web UI opts into bidirectional paging.

### 7. Backward compatibility

- **`ListEventsByTaskRequest`** gains two fields (`page_size`, `page_token`); the response gains
  `prev_page_token` and `next_page_token`. All additive — old clients that send neither request
  field hit the legacy branch and get **exactly today's behavior**: every event, ascending, empty
  tokens. Existing server tests (`event_test.go`, `log_test.go`, `link_test.go`,
  `lifecycle_test.go`, `taskscope_test.go`) that call `ListEventsByTask` with only `TaskId` keep
  passing unchanged.
- **Ordering is unchanged.** Legacy and every paged page return events ascending (`id`), so a
  caller that opts into paging sees the same order it saw before, just in bounded pages.
- **Always-populated `next_page_token`.** In the paged path it is never empty — it doubles as the
  live-follow resume cursor. Clients detect "caught up" from a page shorter than `page_size`, not
  from an empty token. `prev_page_token` follows the usual rule (empty = no older rows).
  Documented on the proto fields.
- **`GetTaskDetails` / `get_my_task`** are untouched — no proto change, no behavior change. The
  agent brief keeps returning every instruction + external event as it does today (see
  [The agent brief](#4b-the-agent-brief-gettaskdetails---unchanged-out-of-scope)).

## Implementation Plan

1. **Proto fields** — Delivers: `page_size`/`page_token` on `ListEventsByTaskRequest`,
   `prev_page_token`/`next_page_token` on the response, with the tail / always-populated / empty
   semantics documented on the fields; regenerate Go + webui (`mise run generate`, buf for webui).
   Depends on: nothing. Verifiable by: generated code compiles; no behavior change yet.
2. **Pagination bidirectional extension** — Delivers: `ErrUnsupportedDirection`, `BiPage`, `ListBi`
   in `internal/pagination`, plus splitting `Source.Query` into `QueryForward`/`QueryBackward` and
   narrowing `limit` from `int32` to `int` (`List` calls `QueryBackward`; `taskSource` keeps its
   query as `QueryBackward` and adds an erroring `QueryForward`) — behavior-preserving. Depends on:
   nothing. Verifiable by: package unit
   tests (mocked `Source`) — newest page, scroll-back to exhaustion (`PrevToken` empties), forward
   follow (`NextToken` always set), empty-forward-poll echo, ascending Items in both directions,
   page-size bounds, undecodable token → `ErrInvalidRequest`; task list still green.
3. **Store paged queries + method** — Delivers: `ListEventsByTaskForward` / `ListEventsByTaskBackward`
   SQL (sqlc-generated), `eventCursor`, `eventSource` (two-directional `Source`), `Store.ListEventsByTaskPage`.
   Depends on: (2) (reuses `idx_events_task_id_id`; no migration). Verifiable by: store unit tests
   against a real DB — walking `prev`/`next` covers the whole stream ascending without gaps/dups; a
   tail `next` token picks up a subsequently-inserted event; `types` filter honored; bad size/token
   → `ErrInvalidRequest`.
4. **`ListEventsByTask` handler paging** — Delivers: the legacy/paged branch + `errors.Is` mapping,
   returning both tokens. Depends on: (1), (3). Verifiable by: handler tests — empty page fields →
   full ascending list (unchanged); empty token + `page_size` → newest page with both tokens;
   walking `prev`/`next` reaches history/tail; invalid page size → `CodeInvalidArgument`.
5. **Web UI bidirectional timeline** — Delivers: `useInfiniteQuery` opening at the tail,
   `fetchPreviousPage` for history (with a "Load older" trigger), `fetchNextPage` on SSE `task_logs`
   for live-follow, trailing-empty-page trimming; drops the timeline's `refetchInterval` and the old
   `invalidateQueries(listEventsByTask)`. Depends on: (4). Verifiable by: opening a task with >
   `page_size` events shows the newest page in one request; scroll-up prepends older pages and stops
   at the oldest; appending an event + firing `task_logs` appends it at the bottom with no reflow;
   idle polls don't grow the page cache; run `pnpm lint`.

## Trade-offs

### Bidirectional tail-start cursor vs one-directional walks

**Chosen: a bidirectional cursor that opens at the tail.** A timeline is opened at "now," read
upward for history, and watched downward for new events — three motions the bidirectional cursor
serves directly: empty token → newest page (O(1) open), `prev_page_token` → older history on
demand, `next_page_token` → the live delta. The rejected alternatives each fail one motion: a
newest-first "load older" cursor needs a *separate* mechanism to discover and append new events at
the head (and reversal to render top-down); an oldest-first forward-only cursor makes live-follow
trivial but forces walking the **entire history on open** (O(N) requests) before the user reaches
current activity. The bidirectional cursor gets O(1) open *and* one-cursor live-follow.

The cost is that `internal/pagination` grows a second entry point (`ListBi`) and two query
shapes. But it is *not* a second, parallel source abstraction — see the next trade-off — and it
is contained in the package (the store and handler stay thin) and is reusable by the next
append-only timeline (e.g. an org or run event feed).

### One `Source` with two query methods (unsupported direction errors)

**Chosen: a single `Source` interface with `QueryForward`/`QueryBackward`, where a source errors
from the method it doesn't implement.** `List` and `ListBi` consume the *same* interface; `List`
calls only `QueryBackward`, `ListBi` calls both. The task source implements `QueryBackward` (its
existing query) and returns `ErrUnsupportedDirection` from `QueryForward`; the event source
implements both. Two alternatives were weighed:

- *A single method taking a `dir Direction` flag* — one method, but the direction becomes a
  runtime parameter to switch on inside every source, and it re-exposes a `Direction` type.
- *Two parallel interfaces / `BiSource` embedding `Source`* — a second interface (compile-time
  proof a one-directional source can't be asked the other way, at the cost of extra surface).

The two-method single interface is the sweet spot: no `dir` flag to switch on (each method has one
job and maps 1:1 to one SQL query), no second interface, and "which directions do I support?" is
answered by which method a source fills in vs. errors. The price is that the guard is a **runtime**
`ErrUnsupportedDirection` rather than a compile-time impossibility — acceptable because `List` only
ever calls `QueryBackward`, so the task source's erroring `QueryForward` can't be reached through
the public entry points. Terminology is generic **forward/backward** (by key order), not domain
newer/older, so the package reads the same for any keyset list; direction survives only as an
unexported bit in the token, so the earlier `Newest`-as-a-third-value concern stays resolved. The
cost elsewhere is that `Source` splits its one `Query` into two and the just-landed `taskSource` /
`List` adopt them — a mechanical, behavior-preserving refactor of the task pagination code.

### Always-populated `next_page_token` vs empty-token-means-done

**Chosen: `next_page_token` always resumes forward; `prev_page_token` empties at history's end.**
An append-only followed stream has no real "done" at the newest end — the tail is only *currently*
the end, and new rows will arrive — so the forward token must survive the tail to act as the
live-follow cursor ("caught up" is signalled by a short page). The older end *is* finite, so
`prev_page_token` empties normally. This asymmetry is baked into `ListBi` rather than the
task-list `List` (whose empty-token-means-done contract stays intact), keeping bounded pagination
(tasks) and followed pagination (events) side by side without either compromising the other.

### Single-column `id` keyset vs `(created_at, id)`

**Chosen: `id` alone.** `events.id` is a unique monotonic `bigserial` and *is* the stream order,
so it is a total order with no tiebreaker — unlike `tasks`, where `created_at` is not unique and
needs `id` appended. This also means the pre-existing `idx_events_task_id_id` covers both scans and
**no migration is needed**. Using `created_at` would be both unnecessary and weaker (ties
possible).

### Legacy path vs migrating every caller to paging

**Chosen: keep the legacy unpaged path.** `ListEventsByTask` has several internal/test callers
that want the whole stream, and `GetTaskDetails` still uses the unpaged store method for the
complete brief. A backward-compatible branch (empty page fields → legacy behavior) lets the RPC
serve both without a flag-day migration of every caller, and since both paths return ascending
there is no ordering seam.

### Leaving the agent brief unbounded vs bounding it now

**Chosen: leave the brief unbounded (out of scope).** The brief is the agent's one-shot task
picture, read once per wake, and it is already narrowed to the two low-volume, load-bearing arms
(instruction + external) — it excludes the high-volume report/lifecycle/link arms that actually
drive timeline growth. Paging it would make the agent issue follow-up calls just to reassemble
its own instructions, and any tail risks dropping context it needs mid-task. The concrete
unbounded-growth problem is the full timeline, which this proposal paginates; the brief keeps its
current behavior. If the brief ever becomes a problem for a pathological task, bounding it is a
separate follow-up (a dedicated paged agent tool, or a head-preserving tail).

### Reusing `internal/pagination` vs an events-specific helper

**Chosen: extend the package.** Adding `ListBi` beside `List` keeps token encode/decode, page-size
bounds, over-fetch, and the `ErrInvalidRequest` contract shared, and the store still adds only an
`eventCursor` and an `eventSource`. An events-specific open-coding was the fallback if the generic
extension got awkward; it did not — the bidirectional logic is regular enough to live generically,
and a followed timeline is a shape the codebase will hit again.

## Open Questions

1. **Default / max page size.** Proposed 50 / 200 (vs the task list's 50 / 100) because timelines
   are denser than task lists. Is 50 the right page size, and is 200 a safe ceiling for a single
   response?
2. **Scroll-up trigger.** History via `fetchPreviousPage` can be a "Load older" button or an
   intersection-observer at the top of the list. Which for the first cut?
3. **Type-filtered scans.** Any future arm-filtered paging scans `(task_id, id)` and filters `type`
   in place. On a task that is overwhelmingly reports, finding N events of a given type may read
   many rows. Acceptable now; if it bites, add `(task_id, type, id)`. Worth pre-empting, or defer
   until measured?
4. **`ListExternalEvents` (org feed).** The org-level external feed (`ListEvents`, bare `limit`, no
   cursor) has the same unbounded shape and could adopt `ListBi` with an org-scoped `Source`. In
   scope as a follow-up, or a separate proposal?
