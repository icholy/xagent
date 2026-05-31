# Link Routing URL

Issue: https://github.com/icholy/xagent/issues/810

## Problem

Event routing matches events to tasks by **exact string equality** on a URL. `FindSubscribedLinksForOrgs` (`internal/store/sql/queries/link.sql`) runs:

```sql
WHERE l.url = sqlc.arg(url) AND l.subscribe = TRUE AND t.archived = FALSE
  AND t.org_id = ANY(...)
```

and `eventrouter.Router.Route` feeds it `input.URL` verbatim. For this to ever match, the URL an agent stores on a link must be byte-identical to the URL a future webhook produces. Two things fall out of that:

1. **Prose burden on the agent.** The prompt (`internal/agent/PROMPT.md`, plus the runtime instructions) spends several lines steering the agent toward the one canonical URL that will match — *"Always use web URLs that users can visit, not API URLs"*, *"ALWAYS set subscribe=true for resources you create"*, etc. This works most of the time but is fragile and constrains how expressive the agent can be when describing a link.

2. **Events can't point at what actually triggered them.** To keep the event URL matchable against agent links, the webhook handlers deliberately collapse every event to its *parent* resource URL (`internal/server/githubserver/webhook.go`):

   | Event | URL stored today |
   |---|---|
   | `issue_comment` | `event.Issue.HTMLURL` (the issue/PR, **not** the comment) |
   | `pull_request_review_comment` | `event.PullRequest.HTMLURL` (the PR, **not** the comment) |
   | `pull_request_review` | `event.PullRequest.HTMLURL` (the PR, **not** the review) |

   So an event that was triggered by a specific comment links to the whole issue. The reviewer/agent loses the deep link to the exact comment or review.

The issue proposes giving links (and, by extension, events) **two URLs**: the original expressive URL the agent created, and a routing URL derived from it that is used solely for event routing.

## Design

### Core idea

Add a second URL — `routing_url` — to both `task_links` and `events`. It is derived from the original `url` by a single, host-aware `model.RoutingURL` function. Routing matches on `routing_url` instead of `url`.

Because **both** sides (the agent-created link and the webhook-produced event) are passed through the *same* `RoutingURL`, neither side has to agree on a canonical form ahead of time:

```
link.routing_url   = RoutingURL(link.url)    // e.g. .../pull/5#issuecomment-9 -> .../pull/5
event.routing_url  = RoutingURL(event.url)   // e.g. .../pull/5#discussion_r3 -> .../pull/5
match: event.routing_url == link.routing_url
```

`url` stays expressive and user-facing (it is what the Web UI links to); `routing_url` is an internal routing key that users never have to think about.

### 1. Routing URL derivation

New code in `internal/model/url.go` (which already houses `TaskURL`):

```go
// RoutingURL reduces an external resource URL to a stable routing key.
// Both link URLs (agent-created) and event URLs (webhook-produced) are passed
// through it, so they match regardless of which sub-resource each side points
// at. Unknown hosts are returned with only the fragment stripped.
func RoutingURL(raw string) string {
    u, err := url.Parse(raw)
    if err != nil || u.Host == "" {
        return raw
    }
    host := strings.ToLower(u.Host)
    switch {
    case host == "github.com":
        return githubRoutingURL(u)
    case strings.HasSuffix(host, ".atlassian.net"):
        return atlassianRoutingURL(u)
    default:
        u.Fragment = ""
        return u.String()
    }
}
```

**GitHub** (`githubRoutingURL`): for a path of the form `/{owner}/{repo}/{pull|issues}/{n}[/...]`, drop everything after `{n}`, drop the fragment/query, and canonicalize the kind segment. GitHub issues and pull requests **share a single number sequence per repo** — number `5` is either an issue or a PR, never both — so `github.com/o/r/issues/5` and `github.com/o/r/pull/5` denote the same entity and can safely collapse to one key (the proposal canonicalizes to `/issues/{n}`; the choice is internal). This handles `#issuecomment-…`, `#discussion_r…`, `#pullrequestreview-…`, `/pull/5/files`, and `/pull/5/commits/…` uniformly.

**Atlassian** (`atlassianRoutingURL`): a Jira browse URL `https://site.atlassian.net/browse/KEY-123?focusedCommentId=…` reduces to `https://site.atlassian.net/browse/KEY-123` by stripping query and fragment.

**Everything else:** strip the fragment and return — a safe no-op for arbitrary links the agent may create (PRs to other services, docs, etc.), which keeps the old behavior (`routing_url == url`).

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

`routing_url` is **always derived by the server** — never accepted from a client — so the single chokepoint is the store's create methods:

```go
// internal/store/link.go
func (s *Store) CreateLink(ctx context.Context, tx *sql.Tx, link *model.Link) error {
    link.RoutingURL = model.RoutingURL(link.URL)
    id, err := s.q(tx).CreateLink(ctx, sqlc.CreateLinkParams{
        TaskID:     link.TaskID,
        Relevance:  link.Relevance,
        Url:        link.URL,
        RoutingUrl: link.RoutingURL,
        Title:      link.Title,
        Subscribe:  link.Subscribe,
        CreatedAt:  link.CreatedAt,
    })
    ...
}
```

`store.CreateEvent` does the same for events. Deriving here guarantees every creation path — the `xagent:create_link` MCP tool, the routing-rule auto-link in `eventrouter.create`, and webhook event creation — stays consistent without each call site remembering to derive it.

Query changes (`link.sql`):

```sql
-- name: CreateLink :one
INSERT INTO task_links (task_id, relevance, url, routing_url, title, subscribe, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: FindSubscribedLinksForOrgs :many
SELECT l.id, l.task_id, l.relevance, l.url, l.routing_url, l.title, l.subscribe, l.created_at, t.org_id
FROM task_links l
JOIN tasks t ON l.task_id = t.id
WHERE l.routing_url = sqlc.arg(routing_url) AND l.subscribe = TRUE
  AND t.archived = FALSE AND t.org_id = ANY(sqlc.arg(org_ids)::BIGINT[])
ORDER BY t.org_id, l.created_at DESC;
```

`event.sql`'s `CreateEvent` and the various `SELECT`s gain the `routing_url` column. `model.Link` and `model.Event` each gain a `RoutingURL string` field (with `Proto` / `FromProto` wiring).

### 4. Router

`eventrouter.Router.Route` (`internal/eventrouter/eventrouter.go`) derives the routing URL once and uses it for the lookup, while persisting the expressive URL on the event:

```go
key := model.RoutingURL(input.URL)
linksByOrg, err := r.links(ctx, key, orgIDs)   // FindSubscribedLinksForOrgs(key, ...)
...
event := &model.Event{
    Description: input.Description,
    Data:        input.Data,
    URL:         input.URL,   // expressive: the comment/review that triggered it
    RoutingURL:  key,         // derived (also set by store.CreateEvent)
    OrgID:       orgID,
}
```

The auto-created subscribed link in `Router.create` is unchanged except that `store.CreateLink` now fills its `routing_url` automatically.

### 5. Webhook handlers — link to the real trigger

With the routing URL handling the matching, the handlers can stop collapsing to the parent and set `URL` to the **actual** comment/review (`internal/server/githubserver/webhook.go`):

| Event | New `InputEvent.URL` | Routing URL |
|---|---|---|
| `issue_comment` | `event.Comment.HTMLURL` | issue/PR URL |
| `pull_request_review_comment` | `event.Comment.HTMLURL` | PR URL |
| `pull_request_review` | `event.Review.HTMLURL` | PR URL |
| `issues`/`pull_request` assigned | parent URL (unchanged — no comment) | parent URL |

Atlassian (`internal/server/atlassianserver/webhook.go`): set `URL` to the comment-focused browse URL when available; its routing URL is the issue. This is the "events link directly to the comments/reviews that triggered them" outcome from the issue.

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

`CreateLinkRequest` is **unchanged** — clients still send only `url`, and the server derives `routing_url`. The Web UI continues to render `url`; `routing_url` is available for debugging/display but is not user-authored.

### 7. Prompt simplification

Once matching is based on the routing URL, the prompt no longer needs to coach the agent toward a single matchable URL. In `internal/agent/PROMPT.md` (and the runtime create-link guidance), the strict "use the exact canonical URL or events won't match" framing is replaced with a short note that the agent should link to the most relevant resource (a specific comment, review, PR, or issue) and that the server derives the routing key automatically. "Use web URLs, not API URLs" can stay as a soft preference.

## Trade-offs

- **Stored column vs. derive-at-query-time.** Storing `routing_url` keeps matching a single indexed equality lookup. A Postgres functional/generated index can't express the host-aware Go logic, so the derivation lives in Go and its result is persisted. The cost is a derived column that must be re-derived if the function changes (see Open Questions).
- **Symmetric derivation vs. one canonical authority.** Deriving the routing URL for both link and event URLs with the same function means the webhook layer and the agent never have to share a canonical-URL contract. The alternative (derive only for events, require links to already be canonical) keeps today's prompt burden.
- **Collapsing `pull` ↔ `issues`.** Safe because GitHub uses one number sequence per repo for issues and PRs, so the two path forms always denote the same entity.
- **Backward compatibility.** Existing rows are backfilled with `routing_url = url`. Because the old contract already required canonical parent URLs, those rows keep matching new events without a Go-side backfill.

## Open Questions

1. **Canonical GitHub form.** Collapse to `/issues/{n}` or `/pull/{n}`? It is internal-only, so either works; `/issues/{n}` mirrors the underlying object. Worth fixing one and asserting it in tests.
2. **API-URL tolerance.** Should `RoutingURL` also map `api.github.com/repos/o/r/issues/{n}` → the web key, so an agent that slips and stores an API URL still routes? Nice robustness, slightly more surface area.
3. **Re-derivation on logic change.** If `RoutingURL` evolves, stored keys can drift. Options: a one-shot backfill command, or re-derive link keys lazily. Acceptable to defer, but should be acknowledged.
4. **`FindLinksByURL` / `FindEventsByURL`.** These RPCs/queries still match raw `url`. Leave them exact (they answer "links to this precise URL") or repoint at `routing_url`? Proposed: leave exact for now.
5. **Atlassian comment deep links.** Confirm the focused-comment browse URL format the webhook can construct, and that stripping the query reliably yields the issue key.
