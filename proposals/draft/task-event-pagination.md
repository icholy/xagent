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

Paginate the **timeline** RPC, `ListEventsByTask`, with keyset pagination reusing the existing
`internal/pagination` package. The request gains `page_size`/`page_token`; the response gains
`next_page_token`.

Pagination direction is **oldest-first with a forward cursor**, and the forward walk *is* the
live-follow mechanism:

- The first page is the oldest `page_size` events; `next_page_token` walks **forward** toward
  newer events (`id > cursor`).
- `next_page_token` is **always populated** in the paged path — even a short or empty tail page
  returns the cursor to resume from, so a client that has reached the newest event keeps calling
  `fetchNextPage()` with that token to pick up **appended** events. There is no separate "live
  update" channel for the timeline; polling the forward cursor forward is the update.

This is the opposite of the task list (newest-first, page toward older tasks), and it is chosen
deliberately: a timeline is read top-down (oldest at top, newest at the bottom by the composer),
so oldest-first pages render in place with no reversal, and the same forward cursor that loads
history also fetches the newest delta.

Because the event stream is **append-only** (rows are only ever inserted, with monotonically
increasing `id`), every fully-loaded page is an **immutable window** over a fixed id range. That
removes all of the machinery an in-place-mutating list needs: no page reversal, no
`refetchInterval`, no cache invalidation, and no "is the head still fresh?" bookkeeping. The web
UI walks pages oldest→newest until the tail, then appends the delta on each SSE `task_logs`
signal.

The **agent brief** (`GetTaskDetails`) is deliberately **left unbounded** — it keeps fetching
all instruction + external events exactly as it does today. It is out of scope for this
proposal. See [The agent brief](#4b-the-agent-brief-gettaskdetails---unchanged-out-of-scope).

Pagination mechanics stay owned entirely by the **store**, exactly as for tasks:
`Store.ListEventsByTaskPage` takes the request's `page_size`/`page_token` as plain values and
returns a page of events plus the next token; the cursor type, token encoding, page-size bounds,
and the "fetch one extra, trim" step all live behind the store boundary. The forward-follow
"always emit a resume token" rule is also the store's — layered on top of `pagination.List`
(see [Store method](#store-method)). The handler passes fields through and maps errors.

Keyset (not `LIMIT`/`OFFSET`) is the right fit and is even simpler here than for tasks:
`events.id` is a monotonic, unique `bigserial`, so the cursor is a **single column** (`id`), with
no `(created_at, id)` tiebreaker. The existing `idx_events_task_id_id (task_id, id)` index already
serves the forward range scan, so **no new migration is required**.

### 1. Proto Definitions

`proto/xagent/v1/xagent.proto` — extend the existing request/response with the standard page
fields. `task_id` stays field 1; the pagination fields are additive, so existing callers are
unaffected (see [Backward compatibility](#7-backward-compatibility)):

```protobuf
message ListEventsByTaskRequest {
  int64 task_id = 1;
  int32 page_size = 2;    // Max events per page (default: 50, max: 200). 0 with an empty
                          // page_token selects the legacy unpaged path (all events).
  string page_token = 3;  // Opaque forward cursor from a previous next_page_token; empty for
                          // the first (oldest) page.
}

message ListEventsByTaskResponse {
  repeated Event events = 1;   // Oldest-first (ascending id).
  // Forward cursor to resume from. In the paged path this is ALWAYS set — even a
  // short or empty tail page returns the cursor to resume forward polling from, so
  // a live-following client keeps calling with it to pick up appended events. A
  // page shorter than page_size means the tail is (currently) reached. Empty only
  // in the legacy unpaged path.
  string next_page_token = 2;
}
```

The `page_token` is opaque — the server encodes the keyset (`id`) of the last returned row as
base64, matching the task-list convention. Clients treat it as a blob and pass it back verbatim.

### 2. Pagination Package — one additive export

Events reuse `pagination.List`, `Config`, `Page[T]`, `Source[T, C]`, and `ErrInvalidRequest`
verbatim; `pagination.List`'s behavior is **unchanged** (it still returns an empty `NextToken`
once the last page is reached — the contract the task list relies on to detect "done").

The one addition is a tiny exported helper so a *followed* list can synthesize its always-on
resume token from a cursor, using the same encoding `List` uses internally:

```go
// Token encodes a cursor into an opaque page token — the same encoding List uses
// for NextToken. Exposed for append-only, live-followed lists that must return a
// resume token even at the tail, where List itself yields an empty token. See
// Store.ListEventsByTaskPage.
func Token[C any](c C) (string, error) { return encode(c) }
```

That keeps the "always emit a token" semantics out of the generic `List` (where it would break
the task list) and in the one store method that wants it. The store still adds only an
`eventCursor` struct and an `eventSource` implementation of `Source` — the same shape as
`taskSource`.

### 3. Store Layer

#### SQL query

`internal/store/sql/queries/event.sql` — add a paged variant alongside the retained
`ListEventsByTask`. It keeps the optional `types` filter for parity with the existing query (so
future arm-filtered paging is a param, not a new query), adds a single-column forward keyset
predicate, and bounds with `LIMIT`. Order stays ascending, matching the unpaged query:

```sql
-- name: ListEventsByTaskPage :many
-- A task's events oldest-first (ORDER BY id ASC) for forward keyset pagination
-- and live-follow. The optional types filter narrows to specific arms — an
-- empty/nil array matches all types. use_cursor lets the first page skip the
-- keyset predicate.
SELECT id, org_id, created_at, task_id, type, wake, payload
FROM events
WHERE task_id = sqlc.arg(task_id)
  AND org_id = sqlc.arg(org_id)
  AND (cardinality(sqlc.arg(types)::text[]) = 0 OR type = ANY(sqlc.arg(types)::text[]))
  AND (NOT sqlc.arg(use_cursor)::bool OR id > sqlc.arg(cursor_id)::bigint)
ORDER BY id ASC
LIMIT sqlc.arg(page_limit);
```

Notes:

- **Single-column keyset.** `id` is a unique monotonic `bigserial` (the `events_id_seq` PK), so
  `id > cursor_id` with `ORDER BY id ASC` is a total order — no `created_at` tiebreaker, unlike
  tasks. `id` order *is* insertion (stream) order.
- **No new index.** `idx_events_task_id_id ON events (task_id, id)` already exists and supports
  the forward range scan (`WHERE task_id = ? AND id > ? ORDER BY id ASC LIMIT ?`).
- **Over-fetch/trim** (`page_size + 1`, drop the extra, encode the token from the last returned
  row) happens inside `pagination.List` — the SQL and handler never see it.
- The existing `ListEventsByTask` query is **retained** for the legacy unpaged path and internal
  callers (see [Backward compatibility](#7-backward-compatibility)).

#### Store method

`internal/store/event.go` — mirror `ListTasksPage`, then layer the forward-follow "always return
a resume token" rule on top. Both `eventCursor` and `eventSource` are unexported: callers only
ever see opaque tokens.

```go
// eventCursor is the keyset an event page token encodes. events.id is a unique
// monotonic bigserial, so it is a total order on its own — no tiebreaker.
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
    PageToken string   // opaque forward cursor; empty for the first (oldest) page
}

// eventSource implements pagination.Source for a task's events, oldest-first (forward).
type eventSource struct {
    store  *Store
    tx     *sql.Tx
    params ListEventsByTaskPageParams
}

func (src eventSource) Query(ctx context.Context, cursor *eventCursor, limit int32) ([]*model.Event, error) {
    types := src.params.Types
    if types == nil {
        types = []string{} // nil encodes as SQL NULL; empty array matches the cardinality(...) = 0 guard
    }
    args := sqlc.ListEventsByTaskPageParams{
        TaskID:    src.params.TaskID,
        OrgID:     src.params.OrgID,
        Types:     types,
        UseCursor: cursor != nil,
        PageLimit: limit,
    }
    if cursor != nil {
        args.CursorID = cursor.ID
    }
    rows, err := src.store.q(src.tx).ListEventsByTaskPage(ctx, args)
    if err != nil {
        return nil, err
    }
    return toModelEvents(rows)
}

func (src eventSource) Cursor(e *model.Event) eventCursor {
    return eventCursor{ID: e.ID}
}

func (s *Store) ListEventsByTaskPage(ctx context.Context, tx *sql.Tx, p ListEventsByTaskPageParams) (*pagination.Page[*model.Event], error) {
    src := eventSource{store: s, tx: tx, params: p}
    page, err := pagination.List(ctx, listEventsPaging, p.PageSize, p.PageToken, src)
    if err != nil {
        return nil, err
    }
    // Forward live-follow: always hand back a resume cursor, even at the tail,
    // so a client can keep polling forward for appended rows. pagination.List
    // only sets NextToken when it over-fetched (a full page with more behind it);
    // a short/empty tail page leaves it empty, so fill it here.
    if page.NextToken == "" {
        switch {
        case len(page.Items) > 0:
            // Tail page with rows → resume just after the newest row returned.
            if page.NextToken, err = pagination.Token(src.Cursor(page.Items[len(page.Items)-1])); err != nil {
                return nil, err
            }
        case p.PageToken != "":
            // Empty tail page → resume from the same point the caller was at.
            page.NextToken = p.PageToken
        default:
            // Empty stream, first page → resume from the start (id > 0).
            if page.NextToken, err = pagination.Token(eventCursor{ID: 0}); err != nil {
                return nil, err
            }
        }
    }
    return page, nil
}
```

`pagination.List` builds its own `NextToken` from the **last** returned row, and with
`ORDER BY id ASC` the last row is the **newest** in the page — exactly the forward boundary for
the next page. The store's post-step only fills the token in the three tail cases so the paged
path never returns an empty token.

A bad `PageSize` or undecodable `PageToken` surfaces as a wrapped `pagination.ErrInvalidRequest`;
query failures surface as-is. Same contract as `ListTasksPage`.

### 4. Server Handler (`ListEventsByTask`)

`internal/server/apiserver/event.go` — keep the scope checks, then branch on whether the caller
opted into pagination. When `page_size == 0 && page_token == ""` the handler preserves today's
behavior exactly (all events, oldest-first, no token); otherwise it serves a forward keyset page.

```go
func (s *Server) ListEventsByTask(ctx context.Context, req *xagentv1.ListEventsByTaskRequest) (*xagentv1.ListEventsByTaskResponse, error) {
    caller := apiauth.MustCaller(ctx)
    // ... unchanged scope / instance checks ...

    // Legacy unpaged path: no pagination fields → all events, oldest-first.
    if req.PageSize == 0 && req.PageToken == "" {
        events, err := s.store.ListEventsByTask(ctx, nil, req.TaskId, caller.OrgID, nil)
        if err != nil {
            return nil, connect.NewError(connect.CodeInternal, err)
        }
        return &xagentv1.ListEventsByTaskResponse{Events: model.ProtoMap(events)}, nil
    }

    // Paged path: oldest-first forward keyset page with an always-populated cursor.
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
        NextPageToken: page.NextToken,
    }, nil
}
```

Both paths return events oldest-first, so the client never reorders. The `errors.Is` mapping is
the one place the handler acknowledges pagination, matching `ListTasks`.

### 4b. The agent brief (`GetTaskDetails`) — unchanged, out of scope

`GetTaskDetails` and the agent's `get_my_task` tool are **left exactly as they are**: they keep
fetching the full brief (all instruction + external events, oldest-first) with no bound, no
tail, and no paging.

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

`webui/src/routes/tasks.$id.tsx` switches the timeline from `useQuery(listEventsByTask)` to
`useInfiniteQuery` walking **forward**. The store returns pages oldest-first, and the timeline
renders oldest-at-top (composer at the bottom), so pages flatten directly — **no reversal**:

```tsx
const PAGE_SIZE = 50

const {
  data,
  fetchNextPage,
  isFetchingNextPage,
} = useInfiniteQuery(
  listEventsByTask,
  { taskId, pageSize: PAGE_SIZE },
  {
    pageParamKey: 'pageToken',
    // The paged path always returns a token (it doubles as the live-follow
    // resume cursor), so a truthy token can't mean "stop". We keep it so we can
    // resume forward, and drive auto-advance off page fullness instead.
    getNextPageParam: (lastPage) => lastPage.nextPageToken || undefined,
  },
)

const pages = data?.pages ?? []
const events = pages.flatMap((p) => p.events) // already ascending; no reverse
const timeline = eventsToTimeline(events)

// A page shorter than PAGE_SIZE means the tail is (currently) reached.
const lastPage = pages.at(-1)
const atTail = !lastPage || lastPage.events.length < PAGE_SIZE
```

**Progressive initial load.** On mount, keep pulling until the tail, so the timeline fills in
oldest→newest as pages arrive:

```tsx
useEffect(() => {
  if (!atTail && !isFetchingNextPage) fetchNextPage()
}, [atTail, isFetchingNextPage, fetchNextPage])
```

**Live-follow via the forward cursor.** Once at the tail, the last page's `next_page_token` is
the resume cursor. On each SSE `task_logs` signal for this task, call `fetchNextPage()` once: it
requests `id > <newest seen>` and appends any newly-inserted events at the bottom. Because loaded
full pages are immutable windows over append-only rows, nothing already rendered changes — there
is **no invalidation, no `refetchInterval`, no page reversal, and no head-aware bookkeeping**.
`use-org-sse.ts`'s `task_logs` case changes from `invalidateQueries(listEventsByTask)` to
"`fetchNextPage()` if this task's timeline is at the tail."

**Empty tail pages.** If no new events have been appended since the last poll, `fetchNextPage()`
returns an empty page (whose token echoes the same cursor). Left alone, repeated SSE polls would
accumulate empty pages in the query cache. Handle it by trimming trailing empty pages after each
follow fetch — the preceding page's `next_page_token` already points at the same resume cursor,
so the forward walk is unaffected:

```tsx
// After a follow fetch, drop trailing empty pages so idle polls don't grow the cache.
queryClient.setQueryData(key, (prev) =>
  !prev ? prev : {
    ...prev,
    pages: dropTrailingEmpty(prev.pages),
    pageParams: prev.pageParams.slice(0, dropTrailingEmpty(prev.pages).length),
  },
)
```

(An equivalent alternative is to skip `useInfiniteQuery` for the follow step and issue a manual
forward fetch that appends via `setQueryData` only when it returns rows; trimming keeps the fetch
in `useInfiniteQuery` and is simpler.)

`GetTaskDetails` (task + links) keeps its existing `useQuery` + SSE invalidation unchanged; only
the timeline query moves to the forward-walking infinite model.

### 6. Other callers

`store.ListEventsByTask` (unpaged) is kept and still used by:

- `GetTaskDetails` — for the complete agent brief, unchanged (see
  [The agent brief](#4b-the-agent-brief-gettaskdetails---unchanged-out-of-scope)).
- Tests and any internal consumers that legitimately want the full stream.

Only the web UI timeline moves to the paged path. The `ListEventsByTask` **RPC** serves both:
legacy callers (empty page fields) get the full list; the web UI opts into forward paging.

### 7. Backward compatibility

- **`ListEventsByTaskRequest`** gains two fields (`page_size`, `page_token`); the response gains
  `next_page_token`. All additive — old clients that send neither field hit the legacy branch
  and get **exactly today's behavior**: every event, oldest-first, empty `next_page_token`.
  Existing server tests (`event_test.go`, `log_test.go`, `link_test.go`, `lifecycle_test.go`,
  `taskscope_test.go`) that call `ListEventsByTask` with only `TaskId` keep passing unchanged.
- **Ordering is unchanged.** Both the legacy and paged paths return events oldest-first
  (ascending `id`), so there is no order caveat: a caller that opts into paging sees the same
  ordering it saw before, just in bounded pages.
- **Always-populated token.** The paged path's `next_page_token` is never empty — it doubles as
  the live-follow resume cursor. Clients detect "caught up" from a page shorter than `page_size`,
  not from an empty token. Documented on the proto fields.
- **`GetTaskDetails` / `get_my_task`** are untouched — no proto change, no behavior change. The
  agent brief keeps returning every instruction + external event as it does today (see
  [The agent brief](#4b-the-agent-brief-gettaskdetails---unchanged-out-of-scope)).

## Implementation Plan

1. **Proto fields** — Delivers: `page_size`/`page_token` on `ListEventsByTaskRequest`,
   `next_page_token` on the response, with the tail/always-populated semantics documented on the
   fields; regenerate Go + webui (`mise run generate`, buf for webui). Depends on: nothing.
   Verifiable by: generated code compiles; no behavior change yet.
2. **Pagination export + store paged query & method** — Delivers: the additive `pagination.Token`
   helper; the `ListEventsByTaskPage` SQL query (sqlc-generated); `eventCursor`, `eventSource`,
   `Store.ListEventsByTaskPage` with the always-on forward-follow token. Depends on: nothing
   (reuses `pagination.List` and `idx_events_task_id_id`; no migration). Verifiable by: store unit
   tests — the forward keyset walks the whole stream oldest-first without gaps/dups; the tail page
   returns a resume token that picks up a subsequently-inserted event; an empty poll echoes the
   cursor; token round-trips; `types` filter honored; bad size/token → `ErrInvalidRequest`.
3. **`ListEventsByTask` handler paging** — Delivers: the legacy/paged branch + `errors.Is`
   mapping. Depends on: (1), (2). Verifiable by: handler tests — empty page fields → full
   oldest-first list (unchanged); `page_size` set → oldest-first page + non-empty
   `next_page_token`; walking the token forward reaches and follows the tail; invalid page size →
   `CodeInvalidArgument`.
4. **Web UI forward-walking timeline** — Delivers: `useInfiniteQuery` timeline that fetches pages
   oldest→newest until the tail (progressive render), follows the tail by calling `fetchNextPage()`
   on each SSE `task_logs` signal, and trims trailing empty pages; drops the timeline's
   `refetchInterval` and the old `invalidateQueries(listEventsByTask)` in favor of the forward
   fetch. Depends on: (3). Verifiable by: rendering a task with > `page_size` events fills in
   progressively; appending an event and firing a `task_logs` signal appends it at the bottom with
   no reflow; idle polls don't grow the page cache; run `pnpm lint`.

## Trade-offs

### Oldest-first forward cursor vs newest-first "load older"

**Chosen: oldest-first with a forward cursor.** A task timeline reads top-down — oldest at the
top, newest by the composer at the bottom — and it is append-only. Paging forward matches that
render order (pages drop in with no reversal) and, crucially, the same forward cursor that loads
history also **fetches the live delta**: once at the tail, `next_page_token` is the resume point,
so following the stream is just "keep calling `fetchNextPage()`." A newest-first "load older"
scheme would need a *separate* mechanism to discover and append new events at the head, plus
reversal to render top-down. Forward paging collapses history-loading and live-follow into one
walk.

The cost is that opening a task with a long history walks the whole stream page by page —
bounded per response (`page_size`), but O(N) requests overall to reach the tail. Acceptable for a
first cut: pages render progressively, and each request is cheap and indexed. If it becomes a
problem for very long timelines, a follow-up can "start near the tail" (e.g. a reverse-seek to
the last page, then walk backward for history on demand) without changing the RPC contract.

### Always-populated token (store-owned) vs empty-token-means-done

**Chosen: the paged path always returns a resume token, and that rule lives in the store.**
An append-only followed stream has no real "done" — the tail is only *currently* the end, and new
rows will arrive. So `next_page_token` must survive the tail to serve as the live-follow cursor;
"caught up" is signalled by a short page instead. `pagination.List`'s default (empty token at the
end) is exactly right for the *task list* and must not change, so the always-on rule is layered in
`ListEventsByTaskPage` via the small `pagination.Token` export rather than baked into `List`. This
keeps two genuinely different pagination semantics — bounded (tasks) and followed (events) — side
by side without either compromising the other.

### Single-column `id` keyset vs `(created_at, id)`

**Chosen: `id` alone.** `events.id` is a unique monotonic `bigserial` and *is* the stream order,
so it is a total order with no tiebreaker — unlike `tasks`, where `created_at` is not unique and
needs `id` appended. This also means the pre-existing `idx_events_task_id_id` covers the scan and
**no migration is needed**. Using `created_at` would be both unnecessary and weaker (ties
possible).

### Legacy path vs migrating every caller to paging

**Chosen: keep the legacy unpaged path.** `ListEventsByTask` has several internal/test callers
that want the whole stream, and `GetTaskDetails` still uses the unpaged store method for the
complete brief. A backward-compatible branch (empty page fields → legacy behavior) lets the RPC
serve both without a flag-day migration of every caller, and since both paths return oldest-first
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

**Chosen: reuse.** The package was built generic for exactly this second caller. Events add only
an `eventCursor`, an `eventSource`, and a one-line `pagination.Token` export; `pagination.List`
is unchanged. This is the payoff the task-pagination proposal anticipated ("The same `List` call
backs any future paginated store method (`ListEventsPage`, …)").

## Open Questions

1. **Default / max page size.** Proposed 50 / 200 (vs the task list's 50 / 100) because
   timelines are denser than task lists. Is 50 the right page size for progressive load, and is
   200 a safe ceiling for a single response?
2. **Long-history open cost.** Forward-walking from the oldest event is O(N) requests to reach
   the tail on open. Is that acceptable as the first cut (with "start near the tail" as a later
   optimization), or should the initial jump-to-tail land in this proposal?
3. **Type-filtered scans.** Any future arm-filtered paging scans `(task_id, id)` and filters
   `type` in place. On a task that is overwhelmingly reports, finding N events of a given type
   may read many rows. Acceptable now; if it bites, add `(task_id, type, id)`. Worth pre-empting,
   or defer until measured?
4. **`ListExternalEvents` (org feed).** The org-level external feed (`ListEvents`, bare `limit`,
   no cursor) has the same unbounded shape and could adopt the same `eventSource` pattern (with an
   org-scoped cursor). In scope as a follow-up, or a separate proposal?
