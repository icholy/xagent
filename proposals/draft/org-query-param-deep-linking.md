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

Add a single `?org=<id>` query parameter to **every** route. The URL becomes the source of truth for the active org. A single route guard on the root route compares the URL's `org` against the active token, swaps tokens via the existing `/auth/token?org_id=…` endpoint on mismatch, and clears the React Query cache. Server URLs that point at a resource get the `org` query param appended at construction time.

### Why every route (not just resource detail routes)

The earlier draft of this proposal opted in route-by-route (`/tasks/$id`, `/events/$id`, etc.) and left "cosmetic" routes (`/settings`, `/tasks/new`) alone. Doing it universally simplifies several things:

1. **Single registration point.** `validateSearch` and `beforeLoad` live on `__root.tsx`, not duplicated on every route. New routes inherit org-awareness automatically.
2. **URL is the source of truth.** Every page the user can be on carries the active org in the URL, which means refreshes, back-button, copy-paste, and screenshot-for-a-teammate all preserve the org context.
3. **`xagent_org_id` localStorage can go away.** It's only needed today because the URL doesn't carry the org. With universal `?org`, localStorage degrades to a tiny bootstrap hint for the first navigation after login, and even that can be replaced by "let `ResolveOrg` pick the user's default org" (the existing behaviour when `org_id=0`).
4. **Org switcher is trivial.** It just calls `navigate({ search: { ...prev, org: newId } })`. The root-level guard does the token swap. No more `staticData.orgSwitchRedirect`/`removeQueries`/`invalidateQueries` branching inside the switcher itself — the guard owns that logic.
5. **No "is this route org-scoped?" judgement call.** Today `/settings` doesn't depend on org for *content*, but the user's *token* still does. Keeping the param on `/settings` means the active org is unambiguous everywhere.

The cost is ~10 characters of URL on every page and a search-param on routes that don't strictly need one. That's cheap relative to the duplication we avoid.

### TanStack Router setup

Single declaration on the root route:

```ts
// webui/src/routes/__root.tsx
import { z } from 'zod'

const search = z.object({
  org: z.string().optional(),
})

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient
  auth: AuthTransport
}>()({
  validateSearch: search,
  beforeLoad: ensureOrg,
  component: RootComponent,
})
```

Child routes inherit the `org` search param via `useSearch({ from: '/' })` and don't need to declare it themselves. `beforeLoad` on the root runs before any child route's loaders, so child queries only ever fire against the correct token.

### `ensureOrg` guard

```ts
// webui/src/lib/ensure-org.ts
import { redirect } from '@tanstack/react-router'
import type { AuthTransport } from '@/lib/transport'
import type { QueryClient } from '@tanstack/react-query'

export async function ensureOrg({
  search,
  context: { auth, queryClient },
  location,
}: {
  search: { org?: string }
  context: { auth: AuthTransport; queryClient: QueryClient }
  location: { pathname: string }
}) {
  const wanted = search.org
  if (!wanted) return
  if (wanted === auth.getOrgId()) return

  try {
    await auth.fetchToken(wanted)
  } catch {
    // Not a member of that org — strip the param and let the route render
    // whatever it would normally render under the existing token.
    throw redirect({
      to: location.pathname,
      search: (prev) => ({ ...prev, org: undefined }),
    })
  }
  queryClient.removeQueries()
}
```

Key points:
- Common case (URL `org` matches active token) is a no-op early return.
- `auth.fetchToken(wanted)` already validates membership server-side via `ResolveOrg` (`internal/command/server.go:335`); a 403 throws here.
- A successful swap clears the React Query cache so stale data from the previous org is never displayed.
- `auth` and `queryClient` are injected via `createRootRouteWithContext`, which is the same pattern already used for `queryClient` — no module-level singletons.

### Org switcher in `__root.tsx`

The current switcher (`handleOrgSwitch`) writes to localStorage, calls `fetchToken`, and either redirects to a fallback list page (`staticData.orgSwitchRedirect`) or invalidates queries. After this proposal it just navigates:

```ts
const handleOrgSwitch = (orgId: string) => {
  const redirect = route?.staticData.orgSwitchRedirect
  if (redirect) {
    navigate({ to: redirect, search: { org: orgId } })
  } else {
    navigate({
      to: '.',
      search: (prev) => ({ ...prev, org: orgId }),
      replace: true,
    })
  }
}
```

The `ensureOrg` guard runs after the navigation, fetches the new token, and clears the cache. `staticData.orgSwitchRedirect` stays — it's still useful when switching org from `/tasks/$id`, because task `$id` won't exist in the new org.

### Removing the localStorage org id

`xagent_org_id` is no longer the source of truth, so `transport.ts` simplifies:

- `getOrgId()` reads the JWT claims (decoded once per token swap) instead of localStorage.
- `storeToken()` no longer writes `ORG_ID_KEY`.
- The `orgchange` `EventTarget` machinery and `useSyncExternalStore` in `useOrgId` go away — `useOrgId()` becomes `useSearch({ from: '/' }).org ?? <decoded-from-token>`.
- `useOrgLocalStorage` continues to scope per-org keys using the same `useOrgId()` result, so existing call sites (`tasks.new.tsx`) keep working.

Bootstrap: when there's no `?org` in the URL (e.g. fresh load on `/`), the existing `fetchToken()` with no arg already resolves to the user's default org (see `ResolveOrg` line `335` — `orgID == 0` → default org). The first paint can then read the org from the freshly-issued token.

### Server-generated URLs include `?org`

URLs constructed server-side gain the org param so they survive being pasted into a different browser or shared with a teammate who has access to multiple orgs:

```go
// internal/model/url.go (new)
func TaskURL(baseURL string, taskID, orgID int64) string {
    if baseURL == "" {
        return ""
    }
    return fmt.Sprintf("%s/tasks/%d?org=%d", baseURL, taskID, orgID)
}
```

Call sites:
- `internal/model/task.go:85` — `Task.Proto`.
- `internal/server/mcpserver/mcpserver.go:145,203,264,304` — `report`, link/child creation responses. The MCP base URL has a `/ui` prefix, so the helper variant for MCP is `TaskUIURL`.

Event URLs follow the same pattern when they're materialised in `eventserver`/`apiserver`; today events don't carry a UI URL through proto, so this is a no-op until that surfaces.

### No backend protocol changes

- The token is still scoped to one org at a time.
- `/auth/token?org_id=…` already exists and is the only mechanism used.
- No new RPCs, no DB migrations.

### Web UI changes summary

- `webui/src/routes/__root.tsx`: declare `validateSearch` + `beforeLoad: ensureOrg`, simplify `handleOrgSwitch` to a single `navigate` call, pass `auth` via root context.
- `webui/src/lib/ensure-org.ts`: new guard (above).
- `webui/src/lib/transport.ts`: drop `ORG_ID_KEY`, `orgchange` event, `notifyOrgChange`, and the `lastOrgId` tracking. `getOrgId()` reads from token claims.
- `webui/src/hooks/use-org-id.ts`: read from root search params instead of `useSyncExternalStore`.
- `webui/src/main.tsx` (or wherever `createRouter` runs): inject `auth` into the router context alongside `queryClient`.

### Backwards compatibility

URLs without `?org` keep working: the guard's early-return covers them and the user lands in their default-org token. Old bookmarks, Slack messages, and PR comments are unaffected. The new param is purely additive on read; the change to server-generated URLs only affects URLs *minted after* the change ships.

## Trade-offs

**Universal vs. selective `?org`**: This proposal applies `?org` to every route. The selective alternative — declaring it only on resource detail routes — has the appeal of "param only where it matters," but every route's active org is already implicit in the user's token, so making it explicit in the URL is *more* honest, not less. Universal placement also eliminates the per-route opt-in and the "did we remember to add it?" failure mode for new routes.

**Query param vs. path segment (`/o/:org/tasks/:id`)**: A path-based org scope is more "RESTful" and surfaces the org in URLs without ambiguity, but it requires re-keying every route and every server-generated URL, breaks every existing bookmark, and pushes the org into routes where it's a content-irrelevant prefix. A query param is additive, ignorable when absent, and easy to revert.

**Auto-switch vs. prompt-to-switch**: We could detect the mismatch and show a "This task belongs to org X — switch?" dialog. That's friendlier in the abstract but adds an extra click to every cross-org link and complicates the "click and go" experience that motivates this proposal. Auto-switch matches what users intuitively expect when they click a link.

**Route guard vs. component-level effect**: Doing the switch in `beforeLoad` rather than a `useEffect` inside the component avoids a flash of "wrong org" data and prevents queries firing with the wrong token. The cost is coupling to TanStack Router internals; that coupling already exists for `staticData.orgSwitchRedirect`.

**Drop localStorage vs. keep as fallback**: Universal `?org` makes `xagent_org_id` redundant, since every URL carries the org and `ResolveOrg` already picks the user's default when org id is absent. Keeping localStorage would mean two sources of truth that can drift. Dropping it is simpler.

**Inline org in JWT vs. per-request org header**: Refactoring the API so the org is a per-request value (header or path) would eliminate the token-swap entirely and let the same token serve any org the user belongs to. That's a much larger change and is out of scope here; the query-param approach is forward-compatible — if we move to per-request orgs later, the URLs already carry the org id.

**Server-side URL rewriting vs. client-side**: We could leave `internal/model/task.go` alone and have the UI add the param when constructing internal Link objects. But server-generated URLs (MCP `report` output, agent-emitted URLs that get posted to GitHub/Slack) escape the UI entirely; those must include the org at construction time, so it's simplest to do it consistently on the server.

## Open Questions

- **Do we want a one-time toast when the guard auto-switches orgs?** ("Switched to org X to open this task.") Useful for users with many orgs who might otherwise wonder why the org dropdown changed. Easy to add later.
- **Strip vs. preserve a 403'd `?org`.** The guard strips `?org` if the token fetch fails (user is not a member of that org). An alternative is to leave it in the URL and render a dedicated "you don't have access to org X" page. Stripping is the simpler default and matches the existing "Task not found" UX for cross-org clicks today.
- **Org switching from a route with `orgSwitchRedirect`**: today the switcher branches between "redirect to fallback list" and "stay on current route." Should the redirect itself preserve other search params (filters, pagination) when the resource-detail page falls back to a list? Probably yes, but worth confirming.
