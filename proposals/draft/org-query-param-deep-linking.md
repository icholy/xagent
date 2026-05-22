# Org query param for deep linking

Issue: https://github.com/icholy/xagent/issues/637

## Problem

The active org is stored in `localStorage` (`xagent_org_id`) and baked into the JWT issued by `/auth/token?org_id=…`. Every API call filters by `caller.OrgID` from the token. Deep links like `/tasks/123` don't carry the org, so when a user has multiple orgs and lands on a link for a task in a different org than the one their token is currently scoped to, the backend returns `CodeNotFound` and the UI renders a generic "Task not found" page.

This breaks:
- Server-generated URLs (`internal/model/task.go:85`, `internal/server/mcpserver/mcpserver.go:145,203,264,304`) shared via Slack, email, GitHub PR comments, MCP `report`, etc.
- Sharing a task URL with a teammate whose last-selected org differs.
- Any agent that surfaces a `https://xagent.choly.ca/tasks/N` URL to a human.

The only workaround today is to manually switch orgs in the navbar, by which point the user has lost the original deep link.

## Design

Add an optional `?org=<id>` query parameter to deep-linkable routes. When a route loads, if the URL's `org` does not match the currently scoped org, the UI fetches a new token for that org and re-renders. Server URLs that point at a resource get the `org` query param appended at construction time.

### Routes that take `?org`

The org param applies to any route that displays an org-scoped resource:

| Route              | Notes                            |
|--------------------|----------------------------------|
| `/tasks/$id`       | task detail                      |
| `/events/$id`      | event detail                     |
| `/tasks`           | list — pinning org for share     |
| `/events`          | list                             |
| `/workspaces`      | list                             |
| `/members`         | list                             |
| `/keys`            | list                             |

Routes that have no org-scoped content (`/settings`, `/tasks/new`, `/events/new`) do not need it.

### TanStack Router search schema

Each route that opts in declares an `org` search param via the existing TanStack Router search-validation API:

```ts
// webui/src/routes/tasks.$id.tsx
import { z } from 'zod'

const search = z.object({
  org: z.string().optional(),
})

export const Route = createFileRoute('/tasks/$id')({
  staticData: { orgSwitchRedirect: '/tasks' },
  validateSearch: search,
  beforeLoad: ensureOrg,
  component: TaskDetail,
})
```

`beforeLoad` runs before any route data fetching. Putting the org-switch there means the route's queries only ever fire against the correct token.

### `ensureOrg` route guard

A shared helper in `webui/src/lib/ensure-org.ts`:

```ts
import { redirect } from '@tanstack/react-router'
import { authTransport } from '@/lib/services'
import { queryClient } from '@/lib/services'

export async function ensureOrg({
  search,
}: {
  search: { org?: string }
}) {
  const wanted = search.org
  if (!wanted) return
  if (wanted === authTransport.getOrgId()) return

  try {
    await authTransport.fetchToken(wanted)
  } catch (err) {
    // User is not a member of that org — strip the param and continue
    // with the existing token. Backend will return NotFound and the
    // route renders its "not found" view.
    throw redirect({
      to: location.pathname,
      search: (prev) => ({ ...prev, org: undefined }),
    })
  }
  queryClient.removeQueries()
}
```

Key points:
- The guard is a no-op when the URL's `org` already matches the active one — the common case (user clicks a link in their own org).
- A successful org switch invalidates the entire React Query cache so stale data from the previous org is not displayed.
- The `fetchToken` call already validates org membership server-side via `ResolveOrg` (`internal/command/server.go:335`). If the user is not a member of the requested org, the backend returns 403, the guard strips the bogus `org` param, and the user sees the existing "Task not found" view in their actual org.
- The `org` param is honoured for one navigation: after the switch the param is preserved in the URL so a refresh or copy-paste still routes correctly.

### Sync with org switcher

The `__root.tsx` org switcher (`handleOrgSwitch`) currently writes the new org to localStorage and either redirects to a fallback list page or invalidates queries. After this proposal it also updates the URL:

```ts
const handleOrgSwitch = async (orgId: string) => {
  await auth.fetchToken(orgId)
  const redirect = route?.staticData.orgSwitchRedirect
  if (redirect) {
    queryClient.removeQueries()
    await navigate({ to: redirect, search: { org: orgId } })
  } else {
    await navigate({
      to: '.',
      search: (prev) => ({ ...prev, org: orgId }),
      replace: true,
    })
    await queryClient.invalidateQueries()
  }
}
```

Keeping the URL and `localStorage` in sync makes the URL the source of truth while the user is on the page; localStorage remains the fallback for routes that don't declare `org` (the landing case after a fresh login).

### Server-generated URLs include `?org`

URLs constructed server-side gain the org param so they survive being pasted into a different browser or shared with a teammate who has access:

```go
// internal/model/task.go
func (t *Task) Proto(baseURL string) *xagentv1.Task {
    var url string
    if baseURL != "" {
        url = fmt.Sprintf("%s/tasks/%d?org=%d", baseURL, t.ID, t.OrgID)
    }
    // ...
}
```

The same change applies to the four `mcpserver.go` sites that build `%s/ui/tasks/%d`. A small helper avoids duplication:

```go
// internal/model/url.go
func TaskURL(baseURL string, taskID, orgID int64) string {
    if baseURL == "" {
        return ""
    }
    return fmt.Sprintf("%s/tasks/%d?org=%d", baseURL, taskID, orgID)
}
```

(There is also an MCP base URL with a `/ui` prefix — keep that prefix in the MCP helper variant.)

Event URLs follow the same pattern when they're materialised in `eventserver`/`apiserver` (currently events don't carry a UI URL through proto, so this is a no-op until that surfaces).

### No backend protocol changes

This proposal does not change the wire protocol or the JWT issuance flow:
- The token is still scoped to one org at a time.
- `/auth/token?org_id=…` already exists and is the mechanism the guard uses.
- No new RPCs, no DB migrations.

### Web UI changes summary

- New file `webui/src/lib/ensure-org.ts` (the guard).
- `webui/src/routes/tasks.$id.tsx`, `events.$id.tsx`, `tasks.index.tsx`, `events.index.tsx`, `workspaces.tsx`, `members.tsx`, `keys.tsx`: add `validateSearch` and `beforeLoad: ensureOrg`.
- `webui/src/routes/__root.tsx`: org switcher also updates URL `org` param.
- `webui/src/lib/services.ts`: export `authTransport` and `queryClient` so the guard can use them outside React (alternatively pass via `createRootRouteWithContext` — see Open Questions).

### Backwards compatibility

URLs without `?org` keep working exactly as today: the guard returns immediately and the route loads against the user's current token. Old bookmarks, old Slack messages, and old PR comments are unaffected. The new param is purely additive.

## Trade-offs

**Query param vs. path segment (`/o/:org/tasks/:id`)**: A path-based org scope is more "RESTful" and surfaces the org in URLs without ambiguity, but it requires re-keying every route and every server-generated URL, breaks every existing bookmark, and forces the org into routes where it's irrelevant (e.g. `/settings`). A query param is additive, optional, and ignorable — much cheaper to roll out and easier to revert.

**Auto-switch vs. prompt-to-switch**: We could detect the mismatch and show a "This task belongs to org X — switch?" dialog. That's friendlier in the abstract but adds an extra click to every cross-org link and complicates the "click and go" experience that motivates this proposal. Auto-switch matches what users intuitively expect when they click a link.

**Route guard vs. component-level effect**: Doing the switch in `beforeLoad` rather than a `useEffect` inside the component avoids a flash of "wrong org" data and prevents queries firing with the wrong token. The cost is coupling to TanStack Router internals; that coupling already exists for `staticData.orgSwitchRedirect`.

**Inline org in JWT vs. per-request org header**: Refactoring the API so the org is a per-request value (header or path) would eliminate the token-swap entirely and let the same token serve any org the user belongs to. That's a much larger change and is out of scope here; the query-param approach is forward-compatible — if we move to per-request orgs later, the URLs already carry the org id.

**Server-side URL rewriting vs. client-side**: We could leave `internal/model/task.go` alone and have the UI add the param when constructing internal Link objects. But server-generated URLs (MCP `report` output, agent-emitted URLs that get posted to GitHub/Slack) escape the UI entirely; those must include the org at construction time, so it's simplest to do it consistently on the server.

## Open Questions

- **Where to expose `authTransport` and `queryClient` to the route guard.** Two options:
  1. Export them as module-level singletons from `webui/src/lib/services.ts` and import directly.
  2. Pass them via `createRootRouteWithContext` (already used for `queryClient`) and read from the route context inside `beforeLoad`. Slightly more boilerplate but plays nicer with tests.
- **Should the org switcher also strip the `org` param when switching to "no redirect" routes?** The proposed implementation keeps the param synced; an alternative is to leave it stale on those routes and let `ensureOrg` re-apply it next time. Keeping it synced is less surprising.
- **Do we want a one-time toast when the guard auto-switches orgs?** ("Switched to org X to open this task.") Useful for users with many orgs who might otherwise wonder why the org dropdown changed. Easy to add later if needed.
- **Truncated org in URL when not a member.** The guard strips `?org` if the token fetch fails. An alternative is to leave it and let the user see a clearer "you don't have access to org X" page. The simpler behaviour is fine for now since 403 from the token endpoint is rare.
