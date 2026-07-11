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
reuses those conventions for the per-task event stream, adapting them for a **timeline**:
newest-first, "load older", with a live-growing head.

## Design

### Overview

Paginate the **timeline** RPC, `ListEventsByTask`, with keyset pagination reusing the existing
`internal/pagination` package. The request gains `page_size`/`page_token`; the response gains
`next_page_token`. Pagination direction is **newest-first**: the first page is the newest
`page_size` events, and `next_page_token` walks toward **older** events ("load older") — the
opposite of the task list, which pages toward older *tasks* but whose first page is also the
newest. The web UI switches the timeline to `useInfiniteQuery` with a "Load older" control.

The **agent brief** (`GetTaskDetails`) is deliberately **left unbounded** — it keeps fetching
all instruction + external events exactly as it does today. It is out of scope for this
proposal. See [The agent brief](#4b-the-agent-brief-gettaskdetails).

Pagination mechanics stay owned entirely by the **store**, exactly as for tasks:
`Store.ListEventsByTaskPage` takes the request's `page_size`/`page_token` as plain values and
returns a page of events plus the next token; the cursor type, token encoding, page-size
bounds, and the "fetch one extra, trim" step all live behind the store boundary via
`pagination.List`. The handler passes fields through and maps errors.

Keyset (not `LIMIT`/`OFFSET`) is the right fit for the same reason it was for tasks — the
stream grows at the head while the UI polls — and it is even simpler here: `events.id` is a
monotonic, unique `bigserial`, so the cursor is a **single column** (`id`), with no
`(created_at, id)` tiebreaker. The existing `idx_events_task_id_id (task_id, id)` index already
serves the reverse scan, so **no new migration is required**.

### 1. Proto Definitions

`proto/xagent/v1/xagent.proto` — extend the existing request/response with the standard page
fields. `task_id` stays field 1; the pagination fields are additive, so existing callers are
unaffected (see [Backward compatibility](#7-backward-compatibility)):

```protobuf
message ListEventsByTaskRequest {
  int64 task_id = 1;
  int32 page_size = 2;    // Max events per page (default: 50, max: 200). 0 with an empty
                          // page_token selects the legacy unpaged path (all events, oldest-first).
  string page_token = 3;  // Opaque cursor from a previous next_page_token; empty for the first page.
}

message ListEventsByTaskResponse {
  repeated Event events = 1;
  string next_page_token = 2;  // Token for the next (older) page; empty when no older events remain.
}
```

The `page_token` is opaque — the server encodes the keyset (`id`) of the boundary row as
base64, matching the task-list convention.

### 2. Reused Pagination Package

No changes to `internal/pagination`. Events reuse `pagination.List`, `Config`, `Page[T]`,
`Source[T, C]`, and `ErrInvalidRequest` verbatim. The store method adds only an `eventCursor`
struct and an `eventSource` implementation of `Source` — the same shape as `taskSource`.

### 3. Store Layer

#### SQL query

`internal/store/sql/queries/event.sql` — add a paged variant alongside the retained
`ListEventsByTask`. It keeps the optional `types` filter for parity with the existing query (so
future arm-filtered paging is a param, not a new query), adds a single-column keyset predicate,
flips the order to `DESC`, and bounds with `LIMIT`:

```sql
-- name: ListEventsByTaskPage :many
-- A task's events newest-first (ORDER BY id DESC) for keyset pagination.
-- The optional types filter ($types) narrows to specific arms — an empty/nil
-- array matches all types. use_cursor lets the first page skip the keyset
-- predicate. Callers reverse to chronological order for display.
SELECT id, org_id, created_at, task_id, type, wake, payload
FROM events
WHERE task_id = sqlc.arg(task_id)
  AND org_id = sqlc.arg(org_id)
  AND (cardinality(sqlc.arg(types)::text[]) = 0 OR type = ANY(sqlc.arg(types)::text[]))
  AND (NOT sqlc.arg(use_cursor)::bool OR id < sqlc.arg(cursor_id)::bigint)
ORDER BY id DESC
LIMIT sqlc.arg(page_limit);
```

Notes:

- **Single-column keyset.** `id` is a unique monotonic `bigserial` (the `events_id_seq` PK), so
  `id < cursor_id` with `ORDER BY id DESC` is a total order — no `created_at` tiebreaker, unlike
  tasks. `id` order *is* insertion (stream) order.
- **No new index.** `idx_events_task_id_id ON events (task_id, id)` already exists and supports
  the reverse-ordered range scan (`WHERE task_id = ? AND id < ? ORDER BY id DESC LIMIT ?`).
- **Over-fetch/trim** (`page_size + 1`, drop the extra, encode the token from the last returned
  row) happens inside `pagination.List` — the SQL and handler never see it.
- The existing `ListEventsByTask` query is **retained** for the legacy unpaged path and internal
  callers (see [Backward compatibility](#7-backward-compatibility)).

#### Store method

`internal/store/event.go` — mirror `ListTasksPage`. Both `eventCursor` and `eventSource` are
unexported: callers only ever see opaque tokens.

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
    PageToken string   // opaque token from a previous page; empty for the first page
}

// eventSource implements pagination.Source for a task's events, newest-first.
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
    return pagination.List(ctx, listEventsPaging, p.PageSize, p.PageToken, eventSource{store: s, tx: tx, params: p})
}
```

Because `pagination.List` builds `NextToken` from the **last** returned row and the query is
`ORDER BY id DESC`, the last row is the **oldest** in the page — exactly the boundary for the
next (older) page. The page items come back **newest-first**; the ascending order the timeline
component wants is restored at the presentation layer (see [Web UI](#5-web-ui)), keeping the
store/pagination contract unchanged.

A bad `PageSize` or undecodable `PageToken` surfaces as a wrapped `pagination.ErrInvalidRequest`;
query failures surface as-is. Same contract as `ListTasksPage`.

### 4. Server Handler (`ListEventsByTask`)

`internal/server/apiserver/event.go` — keep the scope checks, then branch on whether the caller
opted into pagination. When `page_size == 0 && page_token == ""` the handler preserves today's
behavior exactly (all events, oldest-first, no token); otherwise it serves a keyset page.

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

    // Paged path: newest-first keyset page.
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

The paged response is newest-first; the client reverses each page for display. The `errors.Is`
mapping is the one place the handler acknowledges pagination, matching `ListTasks`.

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
`useInfiniteQuery`, newest-first, with a "Load older" control. Because the store returns pages
newest-first but the timeline renders **oldest-at-top** (chat style, composer at the bottom),
the loaded pages are flattened in reverse and each page reversed to ascending:

```tsx
const {
  data: eventsData,
  fetchNextPage,       // fetches the next OLDER page
  hasNextPage,
  isFetchingNextPage,
} = useInfiniteQuery(
  listEventsByTask,
  { taskId, pageSize: 50 },
  {
    pageParamKey: 'pageToken',
    getNextPageParam: (lastPage) => lastPage.nextPageToken || undefined,
  },
)

// Pages arrive newest-first (page 0 = newest). Render oldest-at-top: reverse the
// page order, and reverse events within each page back to ascending id.
const events =
  eventsData?.pages.flatMap((p) => [...p.events].reverse()).reverse() ?? []
const timeline = eventsToTimeline(events)
```

A "Load older" button above the timeline calls `fetchNextPage()` while `hasNextPage`, disabled
while `isFetchingNextPage`. (Scroll-anchored auto-load can layer on later without an API change.)

#### Live SSE + the paged timeline

This is the subtle part. Today, an SSE `task_logs` signal invalidates the `listEventsByTask`
query and the whole list is refetched (`webui/src/hooks/use-org-sse.ts`). With a paginated
infinite query that has **more than one page loaded**, a blind invalidate is *incorrect*:
react-query refetches every loaded page with its stored cursor, but new events prepend at the
head, so the head page slides up and the events that fall off its bottom land in a gap the
fixed page-2 cursor no longer covers — they silently vanish from the view until a full reset.

The fix is to keep the live refetch to the case where it is provably correct: **only auto-refetch
while the head page is the only page loaded.** This mirrors how chat UIs behave and eliminates
the boundary gap:

- **Pinned to the head** (no older pages loaded — the default state). On a `task_logs` signal,
  refetch. Only the head page is loaded, its cursor is empty, so the refetch always returns the
  true newest N. New events appear; there is no second page to gap against. **Correct by
  construction.**
- **Scrolled into history** (the user has clicked "Load older", so `pages.length > 1`). Suppress
  the auto-refetch and surface a lightweight "**New events ↓**" affordance instead. Clicking it
  resets the query to a fresh head page (`queryClient.resetQueries` on this task's timeline key)
  and returns the user to the live head. This avoids yanking the user out of the history they
  are reading *and* avoids the boundary gap, because invalidation never runs while multiple
  pages are loaded.

Concretely, `use-org-sse.ts`'s `task_logs` case changes from an unconditional
`invalidateQueries(listEventsByTask)` to a head-aware update: invalidate only when the timeline
query for that task has a single page cached, otherwise set a "new events" flag the timeline
reads. The 60s `refetchInterval` is dropped from the timeline query — SSE already drives
freshness at the head, and a periodic refetch of a multi-page infinite query would reintroduce
the gap.

`GetTaskDetails` (task + links) keeps its existing `useQuery` + SSE invalidation unchanged; only
the timeline query moves to the infinite/head-aware model.

### 6. Other callers

`store.ListEventsByTask` (unpaged) is kept and still used by:

- `GetTaskDetails` — for the complete agent brief, unchanged (see
  [The agent brief](#4b-the-agent-brief-gettaskdetails---unchanged-out-of-scope)).
- Tests and any internal consumers that legitimately want the full stream.

Only the web UI timeline moves to the paged path. The `ListEventsByTask` **RPC** serves both:
legacy callers (empty page fields) get the full oldest-first list; the web UI opts into paging.

### 7. Backward compatibility

- **`ListEventsByTaskRequest`** gains two fields (`page_size`, `page_token`); the response gains
  `next_page_token`. All additive — old clients that send neither field hit the legacy branch
  and get **exactly today's behavior**: every event, oldest-first, empty `next_page_token`.
  Existing server tests (`event_test.go`, `log_test.go`, `link_test.go`, `lifecycle_test.go`,
  `taskscope_test.go`) that call `ListEventsByTask` with only `TaskId` keep passing unchanged.
- **Ordering caveat.** The legacy path is oldest-first; the paged path is newest-first. Order
  therefore depends on whether the caller paginates. This is deliberate — a timeline is read
  newest-first — and is contained: the only order-sensitive consumer is the web UI timeline,
  which reverses pages for display. Documented on the proto fields.
- **`GetTaskDetails` / `get_my_task`** are untouched — no proto change, no behavior change. The
  agent brief keeps returning every instruction + external event as it does today (see
  [The agent brief](#4b-the-agent-brief-gettaskdetails---unchanged-out-of-scope)).

## Implementation Plan

1. **Proto fields** — Delivers: `page_size`/`page_token` on `ListEventsByTaskRequest`,
   `next_page_token` on the response; regenerate Go + webui (`mise run generate`, buf for
   webui). Depends on: nothing. Verifiable by: generated code compiles; no behavior change yet.
2. **Store paged query + method** — Delivers: `ListEventsByTaskPage` SQL query (sqlc-generated),
   `eventCursor`, `eventSource`, `Store.ListEventsByTaskPage`. Depends on: nothing (reuses the
   existing `internal/pagination` package and `idx_events_task_id_id`; no migration). Verifiable
   by: store unit tests — keyset walks the whole stream newest-first without gaps/dups, token
   round-trips, `types` filter honored, bad size/token → `ErrInvalidRequest`.
3. **`ListEventsByTask` handler paging** — Delivers: the legacy/paged branch + `errors.Is`
   mapping. Depends on: (1), (2). Verifiable by: handler tests — empty page fields → full
   oldest-first list (unchanged); `page_size` set → newest-first page + `next_page_token`;
   invalid page size → `CodeInvalidArgument`.
4. **Web UI timeline paging** — Delivers: `useInfiniteQuery` timeline, page reversal, "Load
   older" button. Depends on: (3). Verifiable by: rendering a task with > `page_size` events;
   "Load older" appends older entries above; run `pnpm lint`.
5. **Web UI live-head SSE handling** — Delivers: head-aware `task_logs` invalidation in
   `use-org-sse.ts`, "New events ↓" affordance, drop the timeline's `refetchInterval`. Depends
   on: (4). Verifiable by: with only the head loaded, new events appear on signal; with older
   pages loaded, the affordance shows and reset returns to the live head; no boundary gap.

## Trade-offs

### Newest-first (load older) vs oldest-first (the task-list convention)

**Chosen: newest-first pages, reversed for display.** A timeline's live edge is the newest
event; users open a task to see the latest activity and occasionally scroll back. Paging
oldest-first would put the freshest events on the *last* page — unreachable without walking the
whole history. The cost is that the store returns pages in the opposite order from what the
timeline component renders, so the client reverses; that is a two-line flatten, far cheaper than
paging in the wrong direction. The task list, by contrast, is also newest-first at page 1 but
never needs a live head, so it never needs the reversal.

### Single-column `id` keyset vs `(created_at, id)`

**Chosen: `id` alone.** `events.id` is a unique monotonic `bigserial` and *is* the stream order,
so it is a total order with no tiebreaker — unlike `tasks`, where `created_at` is not unique and
needs `id` appended. This also means the pre-existing `idx_events_task_id_id` covers the scan and
**no migration is needed**. Using `created_at` would be both unnecessary and weaker (ties
possible).

### Legacy oldest-first path vs migrating every caller to paging

**Chosen: keep the legacy unpaged path.** `ListEventsByTask` has several internal/test callers
that want the whole stream oldest-first, and `GetTaskDetails` still uses the unpaged store method
for the complete brief. A backward-compatible branch (empty page fields → legacy behavior) lets the RPC serve both
without a flag-day migration of every caller, at the cost of ordering depending on whether the
caller paginates — contained to one documented proto note and one client that reverses anyway.

### Leaving the agent brief unbounded vs bounding it now

**Chosen: leave the brief unbounded (out of scope).** The brief is the agent's one-shot task
picture, read once per wake, and it is already narrowed to the two low-volume, load-bearing arms
(instruction + external) — it excludes the high-volume report/lifecycle/link arms that actually
drive timeline growth. Paging it would make the agent issue follow-up calls just to reassemble
its own instructions, and any tail risks dropping context it needs mid-task. The concrete
unbounded-growth problem is the full timeline, which this proposal paginates; the brief keeps its
current behavior. If the brief ever becomes a problem for a pathological task, bounding it is a
separate follow-up (a dedicated paged agent tool, or a head-preserving tail).

### Head-aware live refetch vs blind invalidate vs append-from-SSE

**Chosen: head-aware refetch.** Blindly invalidating a multi-page infinite query drops
head-boundary events into a gap the fixed lower cursors miss — a real correctness bug under rapid
appends. Appending event payloads straight from SSE would avoid refetches entirely but the SSE
channel only carries lightweight "resource changed" signals (`use-org-sse.ts`), not payloads, so
it would require enlarging the notification protocol. Gating auto-refetch to the head-only case
(the common one) is correct by construction and needs no protocol change; the "scrolled into
history" case trades an automatic update for a one-click "New events" affordance, which is the
expected chat-timeline behavior anyway.

### Reusing `internal/pagination` vs an events-specific helper

**Chosen: reuse.** The package was built generic for exactly this second caller. Events add only
an `eventCursor` and an `eventSource` — no changes to `pagination.List`. This is the payoff the
task-pagination proposal anticipated ("The same `List` call backs any future paginated store
method (`ListEventsPage`, …)").

## Open Questions

1. **Default / max page size.** Proposed 50 / 200 (vs the task list's 50 / 100) because
   timelines are denser than task lists. Is 50 the right first-screen size, and is 200 a safe
   ceiling for a single render?
2. **Type-filtered scans.** Any future arm-filtered paging scans `(task_id, id)` and filters
   `type` in place. On a task that is overwhelmingly reports, finding N events of a given type
   may read many rows. Acceptable now; if it bites, add `(task_id, type, id)`. Worth pre-empting,
   or defer until measured?
3. **`ListExternalEvents` (org feed).** The org-level external feed (`ListEvents`, bare `limit`,
   no cursor) has the same unbounded shape and could adopt `ListEventsPage` via this same
   `eventSource` pattern (with an org-scoped cursor). In scope as a follow-up, or a separate
   proposal?
4. **Scroll-anchored auto-load.** The first cut uses a "Load older" button. Is an intersection-
   observer auto-load wanted in this proposal, or a later UI-only follow-up?
