# Task links & subscriptions panel

Issue: https://github.com/icholy/xagent/issues/1158

## Problem

A task accumulates **links** — PRs, issues, Jira tickets, and other external
resources — and some of those links are **subscriptions**. A link with
`subscribe=true` tells the event router to route external events on that URL
back to the task: `eventrouter.Router.Route` normalizes the incoming event URL
to a routing key and calls `store.FindSubscribedLinksForOrgs`, which matches
`task_links` rows where `routing_key = $1 AND subscribe = TRUE`
(`internal/eventrouter/eventrouter.go:109-110`,
`internal/store/link.go:43-67`). So `subscribe` is not cosmetic — it is the
exact switch that decides whether a comment on a PR wakes the task.

Today the only place a task's links appear in the Web UI is the activity
timeline on the task detail page (`webui/src/routes/tasks.$id.tsx`). Each link
is rendered as a `Link created` row (`LinkRow` in
`webui/src/components/task-timeline.tsx:193-230`) interleaved with
instructions, reports, and lifecycle events. Two problems follow:

1. **Hard to find at a glance.** The PR/issue links are buried among every
   other timeline entry, ordered by time. There is no single place that answers
   "what is this task attached to, and what is it subscribed to?"
2. **No management.** Links can only be *created*, and only by the agent via the
   `create_link` MCP tool. A human viewing a task cannot add a link, remove a
   stale one, or toggle whether a link is subscribed — even though that toggle
   is precisely what controls event routing.

This proposal adds a dedicated **Links** panel to the task detail page that
lists a task's links at a glance and supports add / remove / toggle-subscribe,
plus the small API surface needed to back it.

## Terminology

The UI should be consistent about two closely-related words:

- **Link** — any external resource associated with a task (`task_links` row).
  Has a `url`, optional `title`, a `relevance` note, and a `subscribe` flag.
- **Subscription** — a link with `subscribe=true`. Not a separate entity; it is
  a link whose toggle is on. Events matching its `routing_key` are routed to the
  task.

The panel therefore shows **links**, and a per-link **Subscribed** toggle is how
a link becomes (or stops being) a subscription. We deliberately avoid a separate
"Subscriptions" list — it would split one row across two places and invite the
misconception that a subscription can exist without a link.

## Current state

### Data model

`task_links` (`internal/store/sql/schema.sql`):

```sql
CREATE TABLE public.task_links (
    id          bigint NOT NULL,
    task_id     bigint NOT NULL,
    relevance   text   NOT NULL,
    url         text   NOT NULL,
    title       text   DEFAULT ''::text  NOT NULL,
    subscribe   boolean DEFAULT false    NOT NULL,
    created_at  timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    routing_key text   DEFAULT ''::text  NOT NULL
);
```

`routing_key` is derived from `url` by `model.RoutingKey`
(`internal/model/url.go:32-63`), which canonicalizes recognized GitHub/Jira
URLs (strips `#fragment`, `?query`, normalizes API vs web forms) so a comment
URL, web URL, and API URL for the same resource all match. It is **server-owned
and read-only** to clients (`TaskLink.routing_key` is documented as read-only in
the proto).

### API surface that exists today

Proto (`proto/xagent/v1/xagent.proto`):

| RPC | Purpose | Notes |
| --- | --- | --- |
| `CreateLink(CreateLinkRequest) → CreateLinkResponse` | add a link | fields: `task_id, relevance, url, title, subscribe` |
| `ListLinks(ListLinksRequest) → ListLinksResponse` | list a task's links | `task_id` in, `repeated TaskLink` out |
| `GetTaskDetails(...) → GetTaskDetailsResponse` | task + events + links | returns `repeated TaskLink links = 4` |

```proto
message TaskLink {
  int64 id = 1;
  int64 task_id = 2;
  string relevance = 3;
  string url = 4;
  string title = 5;
  bool subscribe = 6;
  google.protobuf.Timestamp created_at = 7;
  string routing_key = 8; // read-only, server-derived
}
```

Handlers live in `internal/server/apiserver/link.go`:

- `CreateLink` (`link.go:17-79`) authorizes `OpTaskWrite`, inserts the
  `task_links` row **and** appends a `link` event to the timeline in one
  transaction (`store.WithTx`), then publishes a `change` notification with
  resources `task_links` and `link`.
- `ListLinks` (`link.go:81-108`) authorizes `OpTaskRead` and returns
  `store.ListLinksByTask`.

### The gap

There is **no `DeleteLink` RPC and no `UpdateLink` RPC**. Interestingly the
store method already exists — `store.DeleteLink` (`internal/store/link.go:39-41`,
SQL `DELETE FROM task_links WHERE id = $1`) — but nothing in the apiserver calls
it. There is no way at all to change `subscribe` on an existing row. So the
management half of this feature is a genuine API gap, not just a UI gap.

### How the Web UI fetches and stays live

- The task detail page calls `useQuery(getTaskDetails, { id }, { refetchInterval: 60000 })`
  and `useQuery(listEventsByTask, { taskId }, ...)`
  (`webui/src/routes/tasks.$id.tsx:55-67`). `getTaskDetails` already returns
  `links`, but the page currently ignores that field and renders links only from
  the events timeline.
- Mutations follow `useMutation(rpc, { onSuccess: refetchAll })` and call
  `mutateAsync(input)` (`tasks.$id.tsx:74-105`, e.g. the instruction composer
  using `updateTask`).
- Live updates: `useOrgSSE` (`webui/src/hooks/use-org-sse.ts`) listens to the
  server's SSE notification stream and invalidates TanStack Query keys per
  resource. The `task_links` case already invalidates `getTaskDetails`
  (`use-org-sse.ts:44-52`). So any RPC that publishes a `task_links` `change`
  notification will refresh this panel on every connected client for free.
- Generated Connect-Query hooks are re-exported from
  `webui/src/gen/xagent/v1/xagent-XAgentService_connectquery.ts`; `createLink`
  and `listLinks` are already generated there.
- shadcn/ui components already in `webui/src/components/ui/`: `Card`, `Dialog`,
  `Button`, `Input`, `Label`, `Textarea`, `Switch`, `Badge`, `Tooltip` — enough
  to build the panel with no new primitives.

## Design

### 1. UI placement — a panel on the task detail page

Add a **Links** section to `tasks.$id.tsx`, above the timeline and below the
metadata strip, rather than a separate route.

Rationale:

- Links are inherently *of a task*; a dedicated `/tasks/$id/links` route would
  make the user navigate away from the timeline they were reading, and the data
  (`getTaskDetails.links`) is already loaded on this page.
- It keeps the timeline as the chronological narrative while giving links a
  stable, always-visible home that does not scroll away as the timeline grows.

A `<Tabs>` split (Timeline | Links) was considered and rejected: links are
usually few (0–5), and hiding them behind a tab reintroduces the "where are the
links" problem. A compact always-visible panel is better at the expected sizes.
If a task ever accumulates many links, the panel can collapse older ones behind
a "show all" affordance — noted as an open question, not built up front.

### 2. Layout

A `Card` titled **Links** with an **Add link** button in the header, and one row
per link:

```
┌ Links ─────────────────────────────────── [ + Add link ] ┐
│  ○ fix: close operator shell leg           [Subscribed ●] │  ← github icon, title links to url
│    github.com/icholy/xagent/pull/1149                  🗑 │
│    Opened to resolve the exit hang                        │  ← relevance, muted
│                                                           │
│  ○ PROJ-42  Investigate flaky test         [Subscribed ○] │  ← jira icon
│    acme.atlassian.net/browse/PROJ-42                   🗑 │
└───────────────────────────────────────────────────────────┘
```

Per link, show:

- **Source icon** derived from the URL, reusing `sourceFromUrl` /
  `ExternalSource` already used by the timeline (`github` / `jira` / `other`).
- **Title** (falls back to the URL when empty) as an external `<a>` opening in a
  new tab — same treatment as `LinkRow` today.
- **URL**, muted, below the title.
- **Relevance** note, muted, when present.
- **Subscribed** toggle — a `Switch` with a label. On = events on this URL wake
  the task; the tooltip says exactly that. This is the panel's most important
  control, so it is a labeled switch, not a bare icon.
- **Remove** — a ghost trash `Button`; destructive, so it opens a small confirm
  `Dialog` ("Remove this link? Events for this URL will no longer reach the
  task.").

Empty state: "No links yet. Add a PR, issue, or ticket to associate it with this
task." with the same **Add link** button.

The existing `LinkRow` in the timeline **stays** — it is the historical "a link
was created at time T" record and reads naturally in the narrative. The new
panel is the current-state view of the same underlying rows. (Divergence is not
a concern: the panel reflects `task_links`, the timeline reflects the append-only
`link` events; both already share one source via `CreateLink`'s transaction.)

### 3. Add-link UX

The **Add link** button opens a `Dialog` with a small form:

| Field | Control | Required | Notes |
| --- | --- | --- | --- |
| URL | `Input type=url` | yes | validated non-empty + parseable URL client-side; server re-validates |
| Title | `Input` | no | display label; defaults to the URL when blank |
| Relevance | `Textarea` | no | free-text note, mirrors the MCP tool field |
| Subscribed | `Switch` | no, default **on** | route events for this URL to the task |

`routing_key` is **not** a field — it is server-derived. The dialog may show a
read-only hint ("Events matching this URL will wake the task") when Subscribed
is on, but never lets the user type a routing key.

Submitting calls the existing `CreateLink` RPC (`useMutation(createLink)`),
which already inserts the row, appends the timeline event, and publishes the
`task_links` notification. On success, close the dialog; the SSE invalidation
refreshes the list (with an optional optimistic insert for snappiness).

Default `subscribe=true`: a human adding a PR/issue almost always wants
follow-up events, matching the MCP guidance to subscribe to resources that may
need follow-up.

### 4. Toggle subscribe — a distinct action, not delete-and-recreate

Toggling **Subscribed** must be its own operation, not remove + re-add, because:

- Delete+recreate loses `created_at` and the original timeline `link` event, and
  would emit a spurious second "Link created" entry.
- The routing key is unchanged by the toggle, so nothing about matching needs to
  be rebuilt — only the `subscribe` boolean flips.

This needs a new RPC (see §5). The toggle is optimistic: flip the `Switch`
immediately, call the mutation, and roll back on error.

### 5. API changes

Two new RPCs in `proto/xagent/v1/xagent.proto`, added to `XAgentService`:

```proto
// Remove a link from a task. Idempotent: removing an already-gone link
// succeeds. The task stops receiving events routed via this link.
rpc DeleteLink(DeleteLinkRequest) returns (DeleteLinkResponse);

// Update a mutable field of an existing link. Currently only `subscribe`
// (and optionally title/relevance) may change; url/routing_key are immutable.
rpc UpdateLink(UpdateLinkRequest) returns (UpdateLinkResponse);

message DeleteLinkRequest {
  int64 id = 1;      // link id
  int64 task_id = 2; // for authorization + notification routing
}
message DeleteLinkResponse {}

message UpdateLinkRequest {
  int64 id = 1;
  int64 task_id = 2;
  // Only set fields are applied. subscribe is the primary use.
  optional bool   subscribe = 3;
  optional string title     = 4;
  optional string relevance = 5;
}
message UpdateLinkResponse {
  TaskLink link = 1;
}
```

Notes on shape:

- `task_id` travels on both requests so the handler can load the task for the
  same `OpTaskWrite` authorization + tenancy check `CreateLink` already does
  (`link.go:26-35`), and so the `change` notification can carry the right
  `task_links` id (the notification's `task_links` resource id is the **task
  id**, per `link.go:68`).
- `UpdateLink` uses `optional` scalar fields as a minimal field-mask so a toggle
  sends only `subscribe`. `url` is intentionally **not** updatable — changing a
  URL is semantically a different resource; users delete and re-add instead.
  This keeps `routing_key` derivation a create-time concern only.

Store methods:

- `DeleteLink` **already exists** (`store.DeleteLink`, `internal/store/link.go:39`)
  and its SQL query is generated (`sqlc/link.sql.go`); the handler just needs to
  call it inside `WithTx`.
- Add `store.UpdateLink(ctx, tx, id, subscribe/title/relevance)` with a
  `sqlc`-generated `UPDATE task_links SET subscribe = $2, ... WHERE id = $1`
  query. Because `subscribe` is the only field affecting routing, an
  update-subscribe-only query is sufficient for v1; title/relevance are cheap to
  include in the same statement.

Handlers (`internal/server/apiserver/link.go`), mirroring `CreateLink`:

- `DeleteLink`: authorize → `WithTx` { `store.DeleteLink` } → publish `change`
  with a `task_links` resource (action `deleted`, id = task id) so
  `use-org-sse.ts` invalidates `getTaskDetails`.
- `UpdateLink`: authorize → `WithTx` { `store.UpdateLink` } → publish the same
  `task_links` `change` → return the updated `TaskLink`.

**Timeline events on delete/update.** `CreateLink` appends a `link` event to the
timeline. For consistency, deleting or toggling could append a corresponding
event ("link removed" / "subscription enabled/disabled"). Recommendation: append
a lightweight event for **delete** (so the narrative doesn't silently lose a
resource) and **not** for a subscribe toggle (low-signal, high-churn) — but this
is called out as an open question since it touches the event schema rather than
just the projection. The `task_links` change notification is published
regardless, so the panel stays live either way.

Authorization: both new RPCs gate on `OpTaskWrite` exactly like `CreateLink`;
listing already gates on `OpTaskRead`. No new scopes are introduced.

### 6. Live updates

No new mechanism. All three mutations publish a `change` notification whose
`task_links` resource id is the task id; `use-org-sse.ts:44-52` already maps that
to a `getTaskDetails` invalidation, and the panel reads `getTaskDetails.links`.
So every connected client's panel refreshes on add / remove / toggle without any
client-specific wiring. The 60 s `refetchInterval` remains the fallback.

The panel should read links from the **`getTaskDetails.links`** field the page
already fetches, rather than adding a second `listLinks` query — one fewer
round-trip and one fewer cache key to invalidate. (`listLinks` remains available
for other consumers such as the CLI.)

### 7. Data source: use `getTaskDetails.links`, drop the timeline as the link source of truth for the panel

The page already loads `getTaskDetails`, which returns `links`. The panel binds
to that array directly. Optimistic updates (add / toggle / remove) mutate the
cached `getTaskDetails` entry; the subsequent SSE invalidation reconciles with
the server. This is the same optimistic-then-invalidate pattern the instruction
composer implies.

## Scope

**In scope:** the Links panel (list + add + remove + toggle-subscribe), the
`DeleteLink` and `UpdateLink` RPCs and their store/handler code, and the proto
`optional` field-mask on update.

**Out of scope:**

- Redesigning event matching / routing keys. This surfaces and edits
  `subscribe`; it does not change how `FindSubscribedLinksForOrgs` matches.
- **Showing which events matched a subscription.** That information already
  lives in the timeline as `external` events, and correlating an event back to
  the specific link that routed it is a bigger feature (the router attaches by
  routing key, not by link id — `eventrouter.go:123-138`). Recommend leaving it
  in the timeline for now; a future "N events" affordance per link is noted
  below.
- Editing a link's `url` (delete + re-add instead, to keep `routing_key`
  derivation create-only).

## Trade-offs

- **Panel on the detail page vs. a dedicated route.** The panel keeps links next
  to the timeline and reuses already-fetched data; a route would isolate the
  feature but cost a navigation and a separate fetch. For 0–5 links per task the
  panel wins. Chosen: panel.
- **`UpdateLink` field-mask vs. a dedicated `SetLinkSubscribe` RPC.** A narrow
  `SetLinkSubscribe(id, subscribe)` is simpler to implement but ossifies as soon
  as we want to edit title/relevance. A small `optional`-field `UpdateLink`
  covers the toggle today and title/relevance edits later with one RPC. Chosen:
  `UpdateLink`.
- **Toggle as update vs. delete+recreate.** Delete+recreate reuses the existing
  `CreateLink`/`DeleteLink` and needs no new RPC, but churns `created_at`,
  double-logs the timeline, and briefly drops the subscription. An in-place
  update is correct and cheap. Chosen: update.
- **Keeping the timeline `LinkRow` vs. removing it.** Removing it would avoid two
  representations of a link, but the timeline row is a genuine historical event
  ("added at T") while the panel is current state. They answer different
  questions; both stay.
- **Reading `getTaskDetails.links` vs. a dedicated `listLinks` query.** Reusing
  the page's existing query avoids a second fetch and a second invalidation
  path; the trade-off is coupling the panel to `getTaskDetails`, which is
  acceptable since they share a page and a notification resource.

## Open questions

1. **Delete/toggle timeline events.** Should removing a link or flipping
   `subscribe` append a timeline event (for an audit trail), or only update the
   projection + notification? Leaning: event on delete, none on toggle.
2. **Confirm-on-remove.** Is a confirm dialog warranted, or is remove cheap
   enough (re-addable) to be a one-click action with undo? Leaning: confirm,
   because removing a subscription silently stops event routing.
3. **Many-links UX.** At what count (10? 20?) should the panel collapse older
   links behind "show all", and should it gain search/sort? Not built up front.
4. **Per-link event count.** Surfacing "N events routed here" per subscription is
   appealing but requires correlating `external` events to a link. Worth a
   follow-up once the router can attribute an event to the link id that matched.
5. **Duplicate URLs.** Should adding a link whose `routing_key` already exists on
   the task be blocked, warned, or allowed? The router already dedupes by task
   when attaching, so duplicates are harmless but noisy; a soft warning may be
   enough.
