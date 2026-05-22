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

Add a single `?org=<id>` query parameter to **every** route. The URL becomes the source of truth for the active org. A single root-level route guard compares the URL's `org` against the active token and *clears* the token on mismatch — it does not fetch a new token itself. The existing on-demand refresh in `transport.fetch()` issues the new token the next time an API call is made, reading the desired org from the URL via `getOrgId()`. A single root-level effect listens for token-org changes via `transport.onOrgChange` and invalidates the React Query cache; that one listener handles both manual switcher clicks and guard-driven swaps. Server URLs that point at a resource get the `org` query param appended at construction time.

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
import type { AuthTransport } from '@/lib/transport'

export function ensureOrg({
  search,
  context: { auth },
}: {
  search: { org?: string }
  context: { auth: AuthTransport }
}) {
  const wanted = search.org
  if (!wanted) return
  if (wanted === auth.tokenOrgId()) return
  auth.clearToken()
}
```

The guard does **two** things in the mismatch case: read the URL, drop the stale token. That's it. No `await`, no `fetchToken`, no `queryClient.removeQueries`. Three reasons:

1. **`transport.fetch()` already refreshes on demand.** When the next API call runs, `getToken()` returns `null` (we just cleared it), `refreshToken()` fires, and `fetchToken()` issues a new token. Calling `fetchToken` with no arg uses `getOrgId()`, which under this proposal reads from the URL (see "Source of truth" below) — so the freshly issued token is for the right org without the guard having to pass anything explicitly.
2. **Membership validation happens inside `ResolveOrg` server-side**, on the refresh call. If the user isn't a member, that refresh returns 403 (or 401), which the existing `transport.fetch` code path surfaces as it does today (redirect to `/auth/login`). No need for the guard to anticipate it.
3. **Cache invalidation isn't the guard's problem.** It's a side effect of the token's org actually changing, which is a property of `transport.fetchToken` — see the next section.

The common case (URL `org` matches the active token) is a single comparison and an early return. The mismatch case is a single `clearToken()` call.

`auth` is injected via `createRootRouteWithContext`, the same pattern already used for `queryClient` — no module-level singletons.

### Single canonical cache-invalidation listener

`AuthTransport` already exposes an `onOrgChange` event, fired from `notifyOrgChange()` inside `storeToken`. Today this is just a re-export of "localStorage org id changed." Under this proposal it becomes "the org embedded in the active token just changed" — which is the *actual* event we care about for cache invalidation.

A single effect at the root listens once:

```ts
// webui/src/routes/__root.tsx (inside RootComponent)
useEffect(
  () => auth.onOrgChange(() => queryClient.removeQueries()),
  [auth, queryClient],
)
```

That's the *only* place cache invalidation lives. Both code paths funnel through it:

- **Guard-driven swap**: URL changes → guard clears token → next API call refreshes → new token issued with different org claim → `onOrgChange` fires → `removeQueries()`.
- **Switcher-driven swap**: user picks org from the dropdown → switcher navigates to new `?org=…` → guard clears token → … same path as above.

So `handleOrgSwitch` in `__root.tsx` no longer contains any cache-invalidation logic. It collapses to a single navigation:

```ts
const handleOrgSwitch = (orgId: string) => {
  const redirect = route?.staticData.orgSwitchRedirect
  navigate(
    redirect
      ? { to: redirect, search: { org: orgId } }
      : { to: '.', search: (prev) => ({ ...prev, org: orgId }), replace: true },
  )
}
```

### Source of truth for "what org should the next token be issued for"

`AuthTransport.fetchToken()` calls `getOrgId()` to decide which org to request. Today that reads `localStorage[xagent_org_id]`. Under this proposal it reads the URL's `?org` instead:

```ts
// webui/src/lib/transport.ts (simplified)
getOrgId(): string {
  const params = new URLSearchParams(window.location.search)
  return params.get('org') ?? NO_ORG
}
```

Reading from `window.location` directly (rather than threading the router into transport) keeps the dependency direction one-way: the router holds the URL; the transport reads from the URL when it needs to. The guard never has to *push* the new org into the transport — when `fetchToken` runs, the URL already reflects the desired org because the navigation that put it there is what triggered the guard in the first place.

`NO_ORG` ("0") is still the fallback when `?org` is absent — `ResolveOrg` interprets it as the user's default org.

Additionally, `AuthTransport` gains a small helper used by the guard:

```ts
// Returns the org id baked into the current token (decoded from JWT claims),
// or null when there is no token. Distinct from getOrgId() which reads the
// URL — they're equal on the happy path, divergent when the URL changed
// faster than the token was refreshed.
tokenOrgId(): string | null { ... }
```

The decode is a single base64-decode of the middle JWT segment plus a `claims.org_id` read; no crypto, no network.

### Removing the localStorage org id

`xagent_org_id` is no longer needed as a storage slot — the URL drives `getOrgId()`, and the token itself carries the org id in its claims for any code that needs to know "what org is the *current session* actually scoped to" (the `onOrgChange` listener, the `useOrgId` hook for display purposes, `useOrgLocalStorage`'s per-org key suffix). Concretely in `transport.ts`:

- `ORG_ID_KEY` and all `localStorage.setItem(ORG_ID_KEY, …)` / `localStorage.removeItem(ORG_ID_KEY)` calls go away.
- `getOrgId()` reads from `window.location.search` as shown above.
- `tokenOrgId()` (new) decodes the JWT for the guard.
- `notifyOrgChange()` still exists, but is fired by `storeToken` when the *token claims* org changes between the previous and the new token (compute the diff against `tokenOrgId()` before assignment).
- `useOrgId()` and `useOrgLocalStorage` continue to read via `useSyncExternalStore` subscribing to `onOrgChange`, so existing call sites (`tasks.new.tsx`) keep working without changes.

Bootstrap: on a fresh load with no `?org` and no token, `transport.fetch()` triggers `refreshToken()` → `fetchToken()` with no arg → `getOrgId()` returns `NO_ORG` → `ResolveOrg` picks the user's default org (`internal/command/server.go:335`). After the token returns, `notifyOrgChange` fires, and `useOrgId()` re-renders with the new value.

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

- `webui/src/routes/__root.tsx`: declare `validateSearch` + `beforeLoad: ensureOrg`, simplify `handleOrgSwitch` to a single `navigate` call, mount the single `auth.onOrgChange(() => queryClient.removeQueries())` effect, pass `auth` via root context.
- `webui/src/lib/ensure-org.ts`: new guard — three lines of real logic (above).
- `webui/src/lib/transport.ts`: `getOrgId()` reads from URL search; drop `ORG_ID_KEY` storage; add `tokenOrgId()` (JWT claim decode); fire `notifyOrgChange` based on token-claim diff rather than localStorage diff.
- `webui/src/hooks/use-org-id.ts`: unchanged structurally (still `useSyncExternalStore` + `onOrgChange`); semantics shift to "current token's org" rather than "stored org id."
- `webui/src/main.tsx` (or wherever `createRouter` runs): inject `auth` into the router context alongside `queryClient`.

### Backwards compatibility

URLs without `?org` keep working: the guard's early-return covers them and the user lands in their default-org token. Old bookmarks, Slack messages, and PR comments are unaffected. The new param is purely additive on read; the change to server-generated URLs only affects URLs *minted after* the change ships.

## Trade-offs

**Universal vs. selective `?org`**: This proposal applies `?org` to every route. The selective alternative — declaring it only on resource detail routes — has the appeal of "param only where it matters," but every route's active org is already implicit in the user's token, so making it explicit in the URL is *more* honest, not less. Universal placement also eliminates the per-route opt-in and the "did we remember to add it?" failure mode for new routes.

**Query param vs. path segment (`/o/:org/tasks/:id`)**: A path-based org scope is more "RESTful" and surfaces the org in URLs without ambiguity, but it requires re-keying every route and every server-generated URL, breaks every existing bookmark, and pushes the org into routes where it's a content-irrelevant prefix. A query param is additive, ignorable when absent, and easy to revert.

**Auto-switch vs. prompt-to-switch**: We could detect the mismatch and show a "This task belongs to org X — switch?" dialog. That's friendlier in the abstract but adds an extra click to every cross-org link and complicates the "click and go" experience that motivates this proposal. Auto-switch matches what users intuitively expect when they click a link.

**Route guard vs. component-level effect**: Doing the swap in `beforeLoad` rather than a `useEffect` inside the component avoids a flash of "wrong org" data and prevents queries firing with the stale token. The cost is coupling to TanStack Router internals; that coupling already exists for `staticData.orgSwitchRedirect`.

**Active fetch in guard vs. clear-and-let-refresh**: An earlier draft had the guard `await auth.fetchToken(wanted)` and then `queryClient.removeQueries()`. That works, but it duplicates the token-refresh path (now there's the guard *and* the 401 handler in `transport.fetch`, each capable of swapping the token) and forces the guard to own cache invalidation. The chosen design has the guard just clear the stale token; refresh happens via the existing on-demand path; invalidation happens via the existing `onOrgChange` event with a single listener at the root. Net: fewer code paths, less duplication, and the guard does not have to be async.

**Drop localStorage vs. keep as fallback**: Universal `?org` makes `xagent_org_id` redundant, since every URL carries the org and `ResolveOrg` already picks the user's default when org id is absent. Keeping localStorage would mean two sources of truth that can drift. Dropping it is simpler.

**Inline org in JWT vs. per-request org header**: Refactoring the API so the org is a per-request value (header or path) would eliminate the token-swap entirely and let the same token serve any org the user belongs to. That's a much larger change and is out of scope here; the query-param approach is forward-compatible — if we move to per-request orgs later, the URLs already carry the org id.

**Server-side URL rewriting vs. client-side**: We could leave `internal/model/task.go` alone and have the UI add the param when constructing internal Link objects. But server-generated URLs (MCP `report` output, agent-emitted URLs that get posted to GitHub/Slack) escape the UI entirely; those must include the org at construction time, so it's simplest to do it consistently on the server.

## Open Questions

- **Do we want a one-time toast when the guard auto-switches orgs?** ("Switched to org X to open this task.") Useful for users with many orgs who might otherwise wonder why the org dropdown changed. Easy to add later.
- **Strip vs. preserve a 403'd `?org`.** The guard strips `?org` if the token fetch fails (user is not a member of that org). An alternative is to leave it in the URL and render a dedicated "you don't have access to org X" page. Stripping is the simpler default and matches the existing "Task not found" UX for cross-org clicks today.
- **Org switching from a route with `orgSwitchRedirect`**: today the switcher branches between "redirect to fallback list" and "stay on current route." Should the redirect itself preserve other search params (filters, pagination) when the resource-detail page falls back to a list? Probably yes, but worth confirming.
