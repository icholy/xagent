# Simplify HTTP authentication dispatch

Issue: https://github.com/icholy/xagent/issues/622

## Problem

`internal/auth/apiauth/apiauth.go` dispatches authentication on a custom `X-Auth-Type` header (`key`, `app`, `bearer`, or absent). This is the original mechanism that let one set of routes serve three different credential types from the CLI, the web UI, and the browser. Today it causes two concrete problems:

### Reactive OAuth refresh is broken for n8n / Claude.ai-style clients

n8n's MCP Client Tool node only refreshes its OAuth2 access token when a request returns `401`. The relevant line in n8n (`packages/@n8n/nodes-langchain/nodes/mcp/shared/utils.ts`) is:

```ts
if (response.status !== 401 || !onUnauthorized) return response;
```

xagent's `RequireAuth` middleware (`internal/auth/apiauth/apiauth.go:266`) reaches the `default:` branch for any request without `X-Auth-Type`. For such requests it tries `validateAppToken`; if the JWT is expired (the app JWT TTL is 5 minutes — see `AppTokenTTL` in `internal/auth/apiauth/jwt.go:23`) it falls through to `a.cookie.RequireAuthentication()`. For a cookieless Bearer request, zitadel's cookie middleware responds with an OIDC login redirect, not a `401`. n8n never sees `401`, never refreshes, and the MCP connection dies after 5 minutes.

A minimal fix has already landed on `master`: in the `default:` branch, if the request carries `Authorization: Bearer …` and the token fails to validate, return `401` instead of falling through to cookie auth. This unblocks n8n and any other OAuth-style client (`pull from xagentclient/token.go`-shaped clients are not affected since they set `X-Auth-Type`).

### `X-Auth-Type` is dead weight everywhere else

After the OAuth 2.1 work (`proposals/accepted/mcp-oauth.md`), three distinct identities can already be told apart from the `Authorization` header alone:

- `Authorization: Bearer xat_…` — API key (the `xat_` prefix is the marker, see `IsKey` in `internal/auth/apiauth/token.go:29`)
- `Authorization: Bearer <jwt>` — app JWT issued by xagent
- no `Authorization` header — cookie session (browser UI)

The fourth case — `Authorization: Bearer <zitadel OIDC access token>` with `X-Auth-Type: bearer` — exists for exactly one consumer: the `xagent setup` command (`internal/command/setup.go:65-78`) does a device-flow login, exchanges the OIDC access token at `GET /auth/token` (via `xagentclient.GetToken` in `internal/xagentclient/token.go:18`) for an app JWT, and then uses that JWT once to call `CreateKey`. After that the CLI persists the `xat_…` key and the zitadel bearer path is never used again from inside this repo.

So the `X-Auth-Type` header, the `AuthTypeBearer` switch case, the zitadel `oauth.IntrospectionContext` middleware, and the entire `xagentclient/token.go` round-trip exist to support a one-time bootstrap that runs once per machine and could just as easily be done in the web UI.

## Design

The end state, in terms of what each non-`/auth/*` route accepts:

| Request | Treatment |
|---|---|
| `Authorization: Bearer xat_…` | Validate as API key |
| `Authorization: Bearer <jwt>` (anything not `xat_`-prefixed) | Validate as app JWT, `401` on failure |
| no `Authorization` header | Cookie session (or dev user, if configured) |

No `X-Auth-Type` header anywhere. No zitadel bearer middleware. No `/auth/token` endpoint. `RequireAuth`/`CheckAuth`/`User` collapse to a single linear branch.

The change is staged so that each phase is independently shippable and reversible. The order matters: the header machinery must be the **last** thing removed so that any external clients still sending `X-Auth-Type: key` keep working until then.

### Phase 1 — Return 401 for invalid Bearer in `RequireAuth` *(already done)*

Already on `master` — see the most recent commit touching `internal/auth/apiauth/apiauth.go` around the `default:` branch of `RequireAuth`. Recording it here so the rest of the plan composes.

The change: in the `default:` branch of `RequireAuth`, if `Authorization: Bearer …` is present and `validateAppToken` returns an error, respond `401` immediately instead of falling through to `a.cookie.RequireAuthentication()`. Cookie auth is only tried when there is no `Authorization` header at all.

This unblocks n8n's reactive refresh, fixes Claude.ai's connector after expiry, and is a no-op for cookie-only and API-key callers.

### Phase 2 — Remove `xagent setup` and its `/auth/token` consumer

Delete:

- `internal/command/setup.go` (entire `SetupCommand`)
- The `setup` subcommand registration wherever `SetupCommand` is wired into the CLI
- `internal/auth/deviceauth/` (only consumer is `setup`; verify with `grep -r deviceauth`)
- The `/device/config` discovery route in `internal/server/server.go:83` and the `handleDeviceConfig` handler, together with the `Discovery` field on `server.Options` and `deviceauth.DiscoveryConfig` plumbing in `internal/command/server.go:216`

After this, nothing in the codebase sends `X-Auth-Type: bearer`. `/auth/token` still exists for the web UI (which calls it via cookie auth — see `webui/src/lib/transport.ts:57`), and that is handled in Phase 3. `xagentclient/token.go` likewise stays alive in Phase 2 since the web UI's flow has its own pathway, but the function `GetToken` becomes unused from Go and can be removed in this phase (it has no callers once `setup.go` is gone).

**Replacement bootstrap path.** New users today need a way to put an API key into `~/.config/xagent/config.yaml` without running `xagent setup`. The proposed replacement, in order of preference:

1. **Web-UI-driven first-login bootstrap.** The web UI already has `webui/src/routes/keys.new.tsx`, which calls the `CreateKey` RPC. Add a small "Quick start: CLI" panel on this page (or as a one-time post-login banner) that:
   - Generates a key (calls `CreateKey` like today)
   - Shows a copy-pasteable `xagent login <key>` command
   - Optionally offers a download of a pre-filled `config.yaml`
2. **New `xagent login <key>` command** (replaces `xagent setup`). It does only what `setup` does after the device flow: generate the private key if missing, write the key into `configfile`, create the default `workspaces.yaml`. No OIDC, no device flow, no token exchange. Implementation is ~30 lines, all already factored out in `internal/configfile` and `internal/runner/workspace`.

   ```
   xagent login --server https://xagent.example.com xat_abcd1234…
   ```

   The flag-driven form means the key can also be piped in from the web UI's "copy to clipboard" button without a follow-up paste.

The current `xagent setup` does three things: SSO login, API key issuance, default config scaffolding. SSO login is no longer needed in the CLI (we have a key); key issuance moves to the UI; config scaffolding stays in `xagent login`.

### Phase 3 — Remove the zitadel bearer middleware and `/auth/token`

Now that nothing sends `X-Auth-Type: bearer`, the middleware and the exchange endpoint can go.

Delete:

- The `bearer` field on `Auth` and the `middleware.New(authZ)` initialization in `internal/auth/apiauth/apiauth.go` (`apiauth.go:130`, `apiauth.go:208`)
- The `authorization.New(...)` block in `New()` (`apiauth.go:200-206`) and the `authorization`/`oauth` imports
- The `AuthTypeBearer` switch cases in `RequireAuth`, `CheckAuth`, and `User`
- `HandleToken` (`apiauth.go:362-408`)
- The `/auth/token` route registration in `internal/server/server.go:85`
- `internal/xagentclient/token.go`

What stays in `apiauth`:

- `authentication.Middleware` / `a.cookie` — still serves the web UI's cookie session
- `validateAppToken` and `validateKey` — both still needed for `Authorization: Bearer …` requests
- The OAuth 2.1 flow in `internal/auth/oauthflow/` and its own `/oauth/token` endpoint (this is a different endpoint, used by Claude.ai-style external clients; see `proposals/accepted/mcp-oauth.md`). It is unaffected by this change.

**Web UI migration.** The web UI currently uses `/auth/token` to swap its cookie session for a short-lived app JWT, then sends that JWT with `X-Auth-Type: app` (`webui/src/lib/transport.ts:55-69, 88`). Once `/auth/token` is gone, the UI cannot do that exchange. Two viable options:

1. **(Recommended) Use cookie auth directly for all API calls.** Drop the `fetchToken`/`refreshToken` plumbing in `AuthTransport`. Send requests with cookie auth and no `Authorization` header. The orgID — currently carried in the app JWT — needs another channel; either:
   - **(Recommended)** A `X-Org-ID` header, resolved server-side by `UserResolver.ResolveOrg` (the same helper `HandleToken` uses today, `apiauth.go:387`). Cookie-authenticated handlers attach the resolved org to `UserInfo.OrgID` exactly as the JWT path does today. The `org_id` query param hack in `transport.ts:57` becomes a header on every request.
   - Or persist the user's selected org in the cookie session itself (the zitadel session supports custom claims, but this couples session shape to xagent state — not worth it).
2. **Issue the app JWT via the OAuth 2.1 endpoints** that already exist (`/oauth/authorize` → `/oauth/token`). This works but is heavier than necessary — the web UI doesn't need PKCE, the consent screen, refresh tokens, etc.

Option 1 is recommended: it removes the dual-token system entirely from the browser, the per-request token refresh in `AuthTransport.fetch`, and the implicit 5-minute reauth that the JWT TTL forced. The UI's existing org-selection UX (`webui/src/routes/__root.tsx` and friends) writes the selected org to `localStorage`; the `X-Org-ID` header is sent from there.

Note: this changes how cookie-authenticated requests determine OrgID. Currently `cookieUser` (`apiauth.go:411-425`) returns `OrgID: 0` and downstream handlers rely on the app-JWT path to populate it. Phase 3 must make `cookieUser` resolve OrgID from `X-Org-ID` via `UserResolver`, otherwise every cookie-auth call hits handlers with `OrgID=0` and fails authorization.

### Phase 4 — Drop the `X-Auth-Type` header

Final cleanup. After this phase, `RequireAuth` and `CheckAuth` are a single branch that reads `Authorization` and dispatches by token shape.

Delete:

- `AuthTypeHeader`, `AuthTypeKey`, `AuthTypeApp`, `AuthTypeBearer` constants in `apiauth.go:24-30`. Keep `AuthTypeCookie` only if `UserInfo.Type` is still consumed somewhere (audit log uses it in `AuditName()` — replace `AuthTypeKey` comparison with a check on the token shape, e.g. an `IsAPIKey bool` field on `UserInfo` set at validation time).
- The `AuthType` field on `xagentclient.AuthTransport` (`internal/xagentclient/transport.go:14`) and the corresponding header set on `transport.go:20`.
- The `AuthType` field on `xagentclient.Options` (`internal/xagentclient/client.go:39`) and its propagation in `New()` (`client.go:61`).
- The `AuthType: "app"` literal in `internal/command/setup.go:87` is already removed in Phase 2.
- The `X-Auth-Type: key` set in `n8n-node/nodes/XAgent/XAgentExecutor.ts:27` (this is the in-repo n8n node; the published `n8n-nodes-xagent` package proposed in `proposals/draft/n8n-community-node.md` does not set this header in its sample code, so the proposal there is already consistent).
- The `X-Auth-Type: app` set in `webui/src/lib/transport.ts:88` (already gone if the web UI moved to cookie auth in Phase 3; otherwise just delete the line).
- All `switch r.Header.Get(AuthTypeHeader)` blocks in `RequireAuth`, `CheckAuth`, and `User`. Replace with a single helper, conceptually:

  ```go
  func (a *Auth) authenticate(r *http.Request) (*UserInfo, error) {
      raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
      if !ok {
          return nil, nil // no Authorization — fall through to cookie middleware
      }
      if IsKey(raw) {
          return a.validator.ValidateKey(r.Context(), HashKey(raw))
      }
      return a.validateAppToken(r) // already returns (*UserInfo, error)
  }
  ```

  `RequireAuth` calls `authenticate`; on `(nil, nil)` it delegates to `a.cookie.RequireAuthentication()`; on `(user, nil)` it attaches the user; on `(_, err)` it responds `401`. `CheckAuth` is the same shape but never `401`s — on auth failure it lets the request through with no user in the context.

## Trade-offs

**Phased rollout vs. one big PR.** Each phase is independently shippable. Phase 1 is already live. Phase 2 is a server-only change with no impact on existing API key holders. Phase 3 changes the web UI's network shape — most risk concentrates here, and it lands separately from anything affecting external clients. Phase 4 is the only phase that breaks any client still sending `X-Auth-Type: key`; by then we know there are none in our own tree, and any external integration that survived without sending it (the new n8n community node, Claude.ai via OAuth, any direct API key user) is unaffected. The cost of phasing is two extra PRs; the benefit is that no single change touches both server middleware and the web UI's auth model.

**`X-Org-ID` header vs. server-managed selected-org.** The header is stateless and matches the existing `org_id=` query parameter pattern at `/auth/token`. The alternative — a server-side "currently selected org" stored per-user — adds a database column and a new RPC, which is overbuilt for "the dropdown in the top right". The org dropdown still drives selection; the header is the wire format.

**Removing `xagent setup` vs. keeping a CLI-driven login.** Removing it is the simpler outcome: no device flow, no OIDC dance, no token exchange round-trip in the CLI. The cost is that first-time users have to copy a key out of the web UI instead of running one command in their terminal. The `xagent login <key>` command preserves the "one CLI command does everything else" UX (config file, private key, workspaces.yaml), it just delegates the *credential acquisition* to the UI. This matches how most modern CLIs (`gh`, `fly`, `vercel`) work for first-time login.

**OIDC token introspection vs. local app JWT validation.** The zitadel bearer middleware was doing token introspection against the zitadel JWKS. Once removed, the only "is this caller authenticated?" check on Bearer headers is local: `Ed25519.Verify` on app JWTs and a DB lookup on `xat_` keys. This is strictly less infrastructure dependency on zitadel for API calls. SSO login (`/auth/login` → `/auth/callback`) still goes through zitadel since the cookie middleware stays.

## Open questions

1. **Dev user shortcut.** `useDevUser` (`apiauth.go:255`) currently bypasses all branches: with `DevUser` set, every request is the dev user regardless of header. The new `RequireAuth` collapses to "Bearer → validate, no Bearer → cookie middleware (or dev user)". A request from the web UI in dev mode arrives with no `Authorization` header, so it hits the cookie/dev-user branch and works. A request from the CLI in dev mode arrives with `Authorization: Bearer xat_…`, hits `validateKey` against an empty database, and `401`s — which is the same behavior as today. Confirmed safe, but the dev-mode `xagent` CLI usage path needs a docs note: in dev mode, the CLI talks to the server *without* an `Authorization` header (so it gets the dev user), or with a real key minted against the dev DB.

2. **Backwards compatibility for `X-Auth-Type: key` callers outside this repo.** As long as Phase 4 lands last, the server happily ignores the header through Phases 1–3 (since `case AuthTypeKey:` still routes them to `validateKey`). Phase 4 silently drops the switch; an external caller that sets the header but uses a valid key still works because the dispatch is by token shape now, and `xat_…` is the shape for keys. The only breakage would be a caller sending `X-Auth-Type: bearer` with a zitadel access token *after* Phase 3, which goes to `validateAppToken`, fails, and gets a `401`. We don't believe any such caller exists outside the repo, but the changelog for the release containing Phase 3 should call this out.

3. **App JWT TTL.** Out of scope here — `AppTokenTTL = 5 * time.Minute` in `internal/auth/apiauth/jwt.go:23` stays as-is. Phase 1 already fixed the n8n bug independent of the TTL, and Phase 3 (web UI cookie auth) removes the only place where a 5-minute TTL was user-visible. The TTL still matters for OAuth-issued app JWTs (Claude.ai), but that's governed by the OAuth proposal, not this one.

4. **OrgID resolution on every request.** Once cookie-authenticated calls resolve OrgID via `X-Org-ID` + `UserResolver.ResolveOrg`, every request does a membership lookup in the DB. Today the app JWT carries `OrgID` and avoids the lookup. If this lookup turns out to be hot enough to matter, it can be cached on the cookie session (zitadel session is in the cookie, but adjacent server-side storage like an LRU keyed by `(user_id, org_id)` would do). Flagged as a follow-up rather than blocking.
