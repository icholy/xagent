# Task links & subscriptions tab

Issue: https://github.com/icholy/xagent/issues/1158

> **Update (2026-07):** the task detail page has since grown an in-page tab bar
> (Timeline | Shell), added by "make task shell an in-page tab" (#1184, building
> on the in-browser shell #1154). That bar is the natural home for links: this
> revision places the feature in a **Links tab** beside Timeline and Shell rather
> than in a standalone card, and reverses the earlier reasoning that rejected a
> tabbed layout. The API/store/live-update design below is unchanged.

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

This proposal adds a dedicated **Links** tab to the task detail page that
lists a task's links at a glance and supports add / remove / toggle-subscribe,
plus the small API surface needed to back it.

## Terminology

The UI should be consistent about two closely-related words:

- **Link** — any external resource associated with a task (`task_links` row).
  Has a `url`, optional `title`, a `relevance` note, and a `subscribe` flag.
- **Subscription** — a link with `subscribe=true`. Not a separate entity; it is
  a link whose toggle is on. Events matching its `routing_key` are routed to the
  task.

The tab therefore shows **links**, and a per-link **Subscribed** toggle is how
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
  (`webui/src/routes/tasks.$id.tsx`). `getTaskDetails` already returns
  `links`, but the page currently ignores that field and renders links only from
  the events timeline.
- The page already has an **in-page tab bar** inside the details card. `#1184`
  introduced local `const [tab, setTab] = useState<TabKey>('timeline')` with
  `type TabKey = 'timeline' | 'shell'`, a reusable `TabButton` component (an
  underline-style tab that takes an `icon`, `label`, and either a `count` badge
  or an "active" `dot`), and a tab bar that renders `<TabButton>` for Timeline
  (with a `count` of timeline items) and Shell (with a `dot` when a shell session
  is active). Each tab's body is a `{tab === '…' && (…)}` block: Timeline renders
  `<TaskTimeline>` plus the instruction composer; Shell renders
  `<TaskShellPanel>`. Adding a Links tab means extending `TabKey`, adding one
  `<TabButton>`, and adding one conditional body — no new layout machinery.
- Mutations follow `useMutation(rpc, { onSuccess: refetchAll })` and call
  `mutateAsync(input)` (e.g. the instruction composer using `updateTask`).
- Live updates: `useOrgSSE` (`webui/src/hooks/use-org-sse.ts`) listens to the
  server's SSE notification stream and invalidates TanStack Query keys per
  resource. The `task_links` case already invalidates `getTaskDetails`
  (`use-org-sse.ts`, `case 'task_links'`). So any RPC that publishes a
  `task_links` `change` notification will refresh this tab on every connected
  client for free.
- Generated Connect-Query hooks are re-exported from
  `webui/src/gen/xagent/v1/xagent-XAgentService_connectquery.ts`; `createLink`
  and `listLinks` are already generated there.
- shadcn/ui components already in `webui/src/components/ui/`: `Card`, `Dialog`,
  `Button`, `Input`, `Label`, `Textarea`, `Switch`, `Badge`, `Tooltip` — enough
  to build the tab with no new primitives.

## Design

### 1. UI placement — a Links tab in the existing tab bar

Add a **Links** tab to the in-page tab bar in `tasks.$id.tsx`, beside Timeline
and Shell, rather than a separate route or a standalone card.

Concretely:

- Extend `type TabKey = 'timeline' | 'shell'` to `'timeline' | 'shell' | 'links'`.
- Add a third `<TabButton>` with a links icon (e.g. `Link2` from `lucide-react`,
  already used by `LinkRow`), the label **Links**, and a `count` of the task's
  links — so the tab shows "Links 3" at a glance, mirroring Timeline's count.
- Add a `{tab === 'links' && (<TaskLinksPanel … />)}` body that renders the list,
  the add-link dialog, and the per-row controls described below.

Rationale:

- The tab bar already exists and is the page's established pattern for "switch
  between views of this task without leaving the page". A links view belongs
  there for the same reason the shell does — it is one more facet of the task,
  not a separate destination.
- Links are inherently *of a task*; a dedicated `/tasks/$id/links` route would
  cost a navigation away from the page and a separate fetch, whereas the data
  (`getTaskDetails.links`) is already loaded here.
- The tab's `count` badge answers "what is this task attached to, and how much?"
  from the bar itself, so links are discoverable even before the tab is opened —
  which addresses the "where are the links" concern that originally argued
  against tabs.

> **Reversal of an earlier decision.** The first draft of this proposal rejected
> a `<Tabs>` split in favour of an always-visible card, reasoning that hiding a
> handful of links behind a tab would reintroduce the "where are the links"
> problem. That reasoning no longer holds: the task page now *is* tab-organized
> (Timeline | Shell), so a standalone card would be the inconsistent choice, and
> the per-tab `count` badge keeps links discoverable from the bar. We therefore
> adopt the tab.

### 2. Layout

The **Links** tab body renders an **Add link** button in a small header row
(aligned like the timeline composer sits at the foot of the Timeline tab) and one
row per link:

```
│ Links  ·  Shell  ·  Timeline                                         │  ← in-page tab bar
├─────────────────────────────────────────────────────── [ + Add link ]┤
│  ○ fix: close operator shell leg                     [Subscribed ●] │  ← github icon, title links to url
│    github.com/icholy/xagent/pull/1149                           🗑 │
│    Opened to resolve the exit hang                                 │  ← relevance, muted
│                                                                    │
│  ○ PROJ-42  Investigate flaky test                   [Subscribed ○] │  ← jira icon
│    acme.atlassian.net/browse/PROJ-42                            🗑 │
```

Per link, show:

- **Source icon** derived from the URL, reusing `sourceFromUrl` /
  `ExternalSource` already used by the timeline (`github` / `jira` / `other`).
- **Title** (falls back to the URL when empty) as an external `<a>` opening in a
  new tab — same treatment as `LinkRow` today.
- **URL**, muted, below the title.
- **Relevance** note, muted, when present.
- **Subscribed** toggle — a `Switch` with a label. On = events on this URL wake
  the task; the tooltip says exactly that. This is the tab's most important
  control, so it is a labeled switch, not a bare icon.
- **Remove** — a ghost trash `Button`; destructive, so it opens a small confirm
  `Dialog` ("Remove this link? Events for this URL will no longer reach the
  task.").

Empty state: "No links yet. Add a PR, issue, or ticket to associate it with this
task." with the same **Add link** button.

The existing `LinkRow` in the timeline **stays** — it is the historical "a link
was created at time T" record and reads naturally in the narrative. The new
tab is the current-state view of the same underlying rows. (Divergence is not
a concern: the tab reflects `task_links`, the timeline reflects the append-only
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
regardless, so the tab stays live either way.

Authorization: both new RPCs gate on `OpTaskWrite` exactly like `CreateLink`;
listing already gates on `OpTaskRead`. No new scopes are introduced.

### 6. Live updates

No new mechanism. All three mutations publish a `change` notification whose
`task_links` resource id is the task id; `use-org-sse.ts` (`case 'task_links'`)
already maps that to a `getTaskDetails` invalidation, and the tab reads
`getTaskDetails.links`. So every connected client refreshes on add / remove /
toggle without any client-specific wiring — and because the tab's `count` binds
to the same `links` array, the tab-bar badge updates too, even while the user is
on another tab. The 60 s `refetchInterval` remains the fallback.

The tab should read links from the **`getTaskDetails.links`** field the page
already fetches, rather than adding a second `listLinks` query — one fewer
round-trip and one fewer cache key to invalidate. (`listLinks` remains available
for other consumers such as the CLI.)

### 7. Data source: use `getTaskDetails.links`, drop the timeline as the link source of truth for the tab

The page already loads `getTaskDetails`, which returns `links`. The tab binds
to that array directly. Optimistic updates (add / toggle / remove) mutate the
cached `getTaskDetails` entry; the subsequent SSE invalidation reconciles with
the server. This is the same optimistic-then-invalidate pattern the instruction
composer implies.

## Scope

**In scope:** the Links tab (list + add + remove + toggle-subscribe), the
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

- **A tab vs. an always-visible card vs. a dedicated route.** A route isolates
  the feature but costs a navigation and a separate fetch. An always-visible card
  (the original proposal) keeps links on screen at all times but is now the
  odd-one-out on a page that organizes Timeline and Shell as tabs, and it would
  compete with the timeline for vertical space. A tab matches the page's existing
  pattern, reuses already-fetched data (`getTaskDetails.links`), and keeps links
  discoverable via the tab's `count` badge without permanently occupying screen
  space. Chosen: tab.
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
  ("added at T") while the tab is current state. They answer different
  questions; both stay.
- **Reading `getTaskDetails.links` vs. a dedicated `listLinks` query.** Reusing
  the page's existing query avoids a second fetch and a second invalidation
  path; the trade-off is coupling the tab to `getTaskDetails`, which is
  acceptable since they share a page and a notification resource.

## Open questions

1. **Delete/toggle timeline events.** Should removing a link or flipping
   `subscribe` append a timeline event (for an audit trail), or only update the
   projection + notification? Leaning: event on delete, none on toggle.
2. **Confirm-on-remove.** Is a confirm dialog warranted, or is remove cheap
   enough (re-addable) to be a one-click action with undo? Leaning: confirm,
   because removing a subscription silently stops event routing.
3. **Many-links UX.** A tab scrolls independently, so a long list is less of a
   problem than it would be in an always-visible card. Still, at some count
   (10? 20?) the tab may want search/sort or grouping (subscribed first). Not
   built up front.
4. **Per-link event count.** Surfacing "N events routed here" per subscription is
   appealing but requires correlating `external` events to a link. Worth a
   follow-up once the router can attribute an event to the link id that matched.
5. **Duplicate URLs.** Should adding a link whose `routing_key` already exists on
   the task be blocked, warned, or allowed? The router already dedupes by task
   when attaching, so duplicates are harmless but noisy; a soft warning may be
   enough.
