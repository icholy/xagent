# Link Routing URL

Issue: https://github.com/icholy/xagent/issues/810

## Problem

Event routing matches events to tasks by **exact string equality** on a URL. `FindSubscribedLinksForOrgs` (`internal/store/sql/queries/link.sql`) runs `WHERE l.url = $1 ...`, and `eventrouter.Router.Route` feeds it `input.URL` verbatim. So the URL an agent stores on a link must be byte-identical to the URL a future webhook produces.

To make that hold, the webhook handlers deliberately collapse every event to its *parent* resource URL (`internal/server/githubserver/webhook.go`):

| Event | URL stored today |
|---|---|
| `issue_comment` | `event.Issue.HTMLURL` (the issue/PR, **not** the comment) |
| `pull_request_review_comment` | `event.PullRequest.HTMLURL` (the PR, **not** the comment) |
| `pull_request_review` | `event.PullRequest.HTMLURL` (the PR, **not** the review) |

An event triggered by a specific comment therefore links to the whole issue, and the agent has to be steered toward the one URL form that will match.

The issue proposes giving links (and events) **two URLs**: the original expressive `url`, and a `routing_url` derived from it that is used solely for routing.

## Design

### Core idea

Add a `routing_url` column to `task_links` and `events`, derived from `url` by `model.RoutingURL`. Routing matches on `routing_url` instead of `url`. Both the agent-created link URL and the webhook-produced event URL are passed through the same `RoutingURL`, so they line up without either side committing to a canonical form. `url` stays expressive and user-facing; `routing_url` is an internal key.

`RoutingURL` only normalizes URLs it recognizes (GitHub issue/PR, Jira issue). Everything else is returned unchanged — there is no general-purpose normalization.

### 1. Routing URL derivation

New code in `internal/model/url.go` (which already houses `TaskURL`):

```go
// RoutingURL reduces a recognized resource URL to a stable routing key, so a
// link and an event that point at different parts of the same resource match.
// Only URLs we recognize are normalized; anything else is returned unchanged.
//
//   github.com/o/r/issues/5#issuecomment-9        -> github.com/o/r/issues/5
//   github.com/o/r/pull/5/files                   -> github.com/o/r/pull/5
//   site.atlassian.net/browse/X-1?focusedCommentId=2 -> site.atlassian.net/browse/X-1
func RoutingURL(raw string) string
```

- **GitHub** `/{owner}/{repo}/{issues|pull}/{n}[/...]`: keep `/{owner}/{repo}/{kind}/{n}`, drop any trailing path, query, and fragment. Issues and pull requests stay separate (`/issues/5` and `/pull/5` are distinct keys). Collapses `#issuecomment-…`, `#discussion_r…`, `#pullrequestreview-…`, `/files`, `/commits/…`.
- **Jira** `/browse/{KEY}[?…]`: keep `/browse/{KEY}`, drop query and fragment.
- **Anything else:** returned unchanged.

### 2. Database schema

New dbmate migration `internal/store/sql/migrations/<ts>_link_routing_url.sql`:

```sql
-- migrate:up
ALTER TABLE task_links ADD COLUMN routing_url TEXT NOT NULL DEFAULT '';
ALTER TABLE events     ADD COLUMN routing_url TEXT NOT NULL DEFAULT '';

-- Existing rows already stored the canonical parent URL (the old contract),
-- so seeding routing_url = url preserves all current matches.
UPDATE task_links SET routing_url = url WHERE routing_url = '';
UPDATE events     SET routing_url = url WHERE routing_url = '';

CREATE INDEX idx_task_links_routing_url ON task_links (routing_url);
CREATE INDEX idx_events_routing_url     ON events (routing_url);

-- migrate:down
DROP INDEX IF EXISTS idx_events_routing_url;
DROP INDEX IF EXISTS idx_task_links_routing_url;
ALTER TABLE events     DROP COLUMN IF EXISTS routing_url;
ALTER TABLE task_links DROP COLUMN IF EXISTS routing_url;
```

The existing `idx_task_links_url` / `idx_events_url` indexes stay (they back the unchanged `FindLinksByURL` / `FindEventsByURL` exact-match queries).

### 3. Store layer

`routing_url` is always derived by the server, never accepted from a client, so the single chokepoint is the store's create methods:

```go
// internal/store/link.go
func (s *Store) CreateLink(ctx context.Context, tx *sql.Tx, link *model.Link) error {
    link.RoutingURL = model.RoutingURL(link.URL)
    // ... INSERT including routing_url ...
}
```

`store.CreateEvent` does the same. Deriving here keeps every creation path — the `xagent:create_link` MCP tool, the routing-rule auto-link in `eventrouter.create`, and webhook event creation — consistent without each call site remembering to derive it.

Query changes (`link.sql`): `CreateLink` inserts `routing_url`; the `SELECT`s return it; `FindSubscribedLinksForOrgs` matches `WHERE l.routing_url = sqlc.arg(routing_url)`. `event.sql`'s `CreateEvent` and `SELECT`s gain the column too. `model.Link` and `model.Event` each gain a `RoutingURL string` field (with `Proto` / `FromProto` wiring).

### 4. Router

`eventrouter.Router.Route` derives the routing URL once for the lookup, and persists the expressive URL on the event:

```go
key := model.RoutingURL(input.URL)
linksByOrg, err := r.links(ctx, key, orgIDs)   // FindSubscribedLinksForOrgs(key, ...)
...
event := &model.Event{
    Description: input.Description,
    Data:        input.Data,
    URL:         input.URL,   // the comment/review that triggered it
    RoutingURL:  key,
    OrgID:       orgID,
}
```

### 5. Webhook handlers — link to the real trigger

With routing handled by `routing_url`, the handlers stop collapsing to the parent and set `URL` to the actual comment/review (`internal/server/githubserver/webhook.go`):

| Event | New `InputEvent.URL` | Routing URL |
|---|---|---|
| `issue_comment` | `event.Comment.HTMLURL` | issue/PR URL |
| `pull_request_review_comment` | `event.Comment.HTMLURL` | PR URL |
| `pull_request_review` | `event.Review.HTMLURL` | PR URL |
| `issues`/`pull_request` assigned | parent URL (unchanged — no comment) | parent URL |

Atlassian (`internal/server/atlassianserver/webhook.go`): set `URL` to the comment-focused browse URL when available; it routes to the issue.

### 6. Proto / API

Add a derived, read-only field to each message in `proto/xagent/v1/xagent.proto`:

```protobuf
message TaskLink {
  // ... existing fields 1-7 ...
  string routing_url = 8;
}

message Event {
  // ... existing fields 1-5 ...
  string routing_url = 6;
}
```

`CreateLinkRequest` is unchanged — clients still send only `url`, and the server derives `routing_url`.

## Trade-offs

- **Stored column vs. derive-at-query-time.** Storing `routing_url` keeps matching a single indexed equality lookup; the host-aware Go logic can't be expressed as a Postgres index expression. The cost is a derived column that must be re-derived if the function changes (see Open Questions).
- **Backward compatibility.** Existing rows are backfilled with `routing_url = url`. Because the old contract already required canonical parent URLs, those rows keep matching new events without a Go-side backfill.

## Open Questions

1. **Re-derivation on logic change.** If `RoutingURL` evolves, stored keys can drift. A one-shot backfill command can re-derive existing rows; acceptable to defer.
2. **`FindLinksByURL` / `FindEventsByURL`.** These still match raw `url`. Proposed: leave them exact (they answer "links to this precise URL"); only routing uses `routing_url`.
3. **Atlassian comment deep links.** Confirm the focused-comment browse URL the webhook can construct, and that `RoutingURL` reliably reduces it to the issue key.
