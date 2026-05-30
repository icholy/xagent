# Custom Zitadel Login UI

Issue: https://github.com/icholy/xagent/issues/703

## Problem

xagent currently delegates the entire login flow to Zitadel Cloud's hosted Login UI (the V2 login served at `/ui/v2/login` on the managed `*.zitadel.cloud` domain). The OIDC code flow is configured in `internal/auth/apiauth/apiauth.go:159-188` via `github.com/zitadel/zitadel-go/v3/pkg/authentication`, with the redirect URI set to `<base>/auth/callback` in `internal/command/server.go:186`. xagent's own server never sees the username/password form — it only receives the authorization code after Zitadel finishes.

The hosted V2 page does not play well with password managers: 1Password, Bitwarden, and the browser-built-in managers frequently fail to autofill the password field, so the user types credentials by hand every time the cookie session expires. The managed branding settings expose logos, colors, and message strings — not autocomplete attributes, form layout, or DOM structure — so this can't be fixed from inside the Zitadel admin UI.

Everything else about the current setup is fine: OIDC is correctly wired, MFA works, and the cookie middleware (`a.cookie` in `apiauth.go:124`) is unchanged by anything below. The replacement only needs to take over what the user *sees and types into* — credential collection, MFA prompts, and the redirect back into the OIDC code flow.

## Design

Build a login page inside our existing `webui/` (React + TanStack Router/Query + shadcn/ui) as a new route, written from scratch and styled like the rest of the app. It talks to Zitadel's Session API and OIDC v2 API through a thin proxy on the xagent server so that the privileged login-client token never reaches the browser. Zitadel's open-source TypeScript login app is used as a *reference* for the flow only — not forked, not vendored.

This keeps the surface area inside the existing repo, deployment, and CI pipeline. There is no new domain, no second hosting target, no upstream fork to track. The only thing that changes about Zitadel itself is one application setting: the Custom Login UI URL points at our `webui` route instead of the managed `/ui/v2/login`.

### Flow

What happens today:

```
Browser
  → GET /auth/login                           (xagent — zitadel-go authentication middleware)
  → 302 to https://<instance>.zitadel.cloud/oauth/v2/authorize?…
  → 302 to https://<instance>.zitadel.cloud/ui/v2/login?authRequest=V2_…   ← the broken page
  → user types credentials, MFA, etc.
  → 302 to <base>/auth/callback?code=…
  → zitadel-go middleware exchanges code, sets cookie session
  → 302 to original URL
```

What happens after this change:

```
Browser
  → GET /auth/login                           (unchanged xagent — zitadel-go authentication middleware)
  → 302 to https://<instance>.zitadel.cloud/oauth/v2/authorize?…
  → 302 to <base>/ui/login?authRequest=V2_…   ← our own React route
  → user types into our form; UI calls our /auth/login/* proxy endpoints
  → on success the proxy returns the Zitadel callback URL; UI navigates there
  → 302 to <base>/auth/callback?code=…
  → zitadel-go middleware exchanges code, sets cookie session  (unchanged)
  → 302 to original URL
```

The first and last steps stay byte-for-byte identical. Only the middle (the page the user types into) moves into `webui/`.

### What needs to live in `xagent` server

A small set of new routes on the xagent server, all under `/auth/login/*`, that proxy the Zitadel Session and OIDC v2 APIs and inject the login-client bearer token server-side. The browser only ever talks to xagent; the PAT for the `IAM_LOGIN_CLIENT`-roled service account stays in server config (added to `apiauth.Config` and read from a CLI flag / env var alongside the existing `auth-domain` / `auth-client-id` in `internal/command/server.go:42-56`).

The endpoints map one-to-one onto Zitadel calls:

| xagent endpoint | Method | Proxies to Zitadel | Purpose |
|---|---|---|---|
| `/auth/login/auth-request/{id}` | GET | `GET /v2/oidc/auth_requests/{id}` | Fetch the OIDC auth request — login hint, prompt, scope, requested IDPs |
| `/auth/login/session` | POST | `POST /v2/sessions` | Start session, check username (`checks.user.loginName`) |
| `/auth/login/session/{id}` | PATCH | `PATCH /v2/sessions/{id}` | Submit password / MFA challenge or check (see *Steps* below) |
| `/auth/login/auth-request/{id}/finalize` | POST | `POST /v2/oidc/auth_requests/{id}` | Tie the authenticated session to the auth request; returns the callback URL |

The proxy is intentionally thin: it forwards JSON bodies untouched, adds `Authorization: Bearer <PAT>`, and returns the upstream response. The session token returned by `POST /v2/sessions` is opaque per-user state, not a privileged credential, so it is fine to round-trip through the browser between calls. The PAT, on the other hand, is the keys-to-the-instance-shaped thing the login-client docs hand you and must never reach JS.

A new file in the same package: `internal/auth/apiauth/login.go`, providing a handler `HandleLogin()` mounted in `internal/server/server.go` next to the existing `/auth/token` and `/auth/` routes:

```go
mux.Handle("/auth/login/", s.auth.HandleLogin())          // new — public, no auth required
mux.Handle("/auth/token", alice.New(s.auth.CheckAuth())…) // unchanged
mux.Handle("/auth/", s.auth.Handler())                    // unchanged — still serves /auth/login, /auth/callback, /auth/logout via the zitadel-go middleware
```

The order matters: `/auth/login/` (with trailing slash, our new proxy prefix) is registered *before* `/auth/` so it wins on `/auth/login/auth-request/…`. The existing zitadel-go route `/auth/login` (no trailing slash, no path beyond it) is untouched — it remains the entry point that issues the initial redirect to Zitadel's `/authorize`.

### What needs to live in `webui/`

A single new route, `/ui/login`, with its own components, served as part of the existing SPA. It reads `?authRequest=V2_…` from the URL, fetches the auth request, walks the user through the necessary steps, and on success follows the callback URL Zitadel returns.

```
webui/src/routes/login.tsx                  — TanStack Router route, top-level layout
webui/src/components/login/
  ├── username-step.tsx                     — step 1: collect email/username
  ├── password-step.tsx                     — step 2: collect password
  ├── mfa-step.tsx                          — step 3 (conditional): TOTP / OTP / passkey
  └── login-form.ts                         — shared form state, autocomplete attrs, submit helpers
webui/src/lib/login-api.ts                  — thin client over /auth/login/* (returns Promise of typed responses)
```

The `__root.tsx` route currently expects an authenticated user (`__root.tsx:60` derives the org from the auth context). The login route must opt out of that. Two ways:

1. Move the org/profile-loading logic out of `__root.tsx` and into a sub-route, leaving `__root.tsx` purely structural. The login route becomes a sibling to the rest of the tree.
2. Add a `beforeLoad` short-circuit in the login route that skips the org logic.

Option 1 is cleaner; option 2 is smaller. Either is fine — this is a webui refactor detail.

Crucially, the login route is **not** behind `s.auth.RequireAuth()`. The current server mounts the SPA as `mux.Handle("/ui/", … s.auth.RequireAuth()(WebUI()))` in `server.go:121`, which means the cookie middleware redirects unauthenticated requests on `/ui/*` straight to Zitadel via `/auth/login`. We carve out `/ui/login` so that one path is reachable without a cookie:

```go
mux.Handle("/ui/login", WebUI())                           // public — no auth middleware
mux.Handle("/ui/", s.auth.RequireAuth()(WebUI()))          // unchanged for everything else
```

The mux matches longest prefix, so `/ui/login` is hit only for that exact route; everything else stays behind cookie auth.

### Steps

The flow inside the React route, with the underlying Zitadel calls noted. All calls go through the xagent proxy described above; this is what the proxy ultimately translates to.

1. **Read `authRequest` from the URL.** Required. If missing, render a generic "please sign in from the app" message — this route should not be reachable except through Zitadel's redirect from `/oauth/v2/authorize`.

2. **Fetch the auth request.** `GET /v2/oidc/auth_requests/{id}`. The response carries the application's settings: `loginHint` (email to prefill), `prompt` (e.g. `login` forces re-auth), requested scopes, and the list of allowed external IDPs. We use this to (a) prefill the username, (b) decide whether the existing-cookie shortcut applies (the user might already have a Zitadel session for this instance — out of scope for v1, see open questions), and (c) render the "sign in with …" buttons if external IDPs are configured.

3. **Username step.** A real `<form>` with `<input type="email" name="username" autocomplete="username">` and a submit button. On submit:
   - `POST /v2/sessions` with `{ "checks": { "user": { "loginName": "<email>" } } }`.
   - Response includes `sessionId`, `sessionToken`, and a `user` object with what factors are available. Store `sessionId` + `sessionToken` in component state (not in `AuthTransport`, which is for our own app JWT — the Zitadel session token is short-lived and round-trips with the password step). On error, surface the message (unknown user, account locked, etc.).

4. **Password step.** Renders the same form structure as step 3 with the username field still present as a hidden, readonly `<input autocomplete="username">` — this is the autofill fix. Adds a `<input type="password" name="password" autocomplete="current-password">`. On submit:
   - `PATCH /v2/sessions/{sessionId}` with `{ "checks": { "password": { "password": "<pwd>" } } }` and `Authorization: Bearer <sessionToken>`.
   - Response gives an updated `sessionToken`. If the response indicates additional factors required, transition to MFA step.

5. **MFA step (conditional).** If the previous response carries a `challenges` requirement or the session is not yet sufficient for the auth request's auth-method policy, run the appropriate challenge/check pair:
   - TOTP: single PATCH with `{ "checks": { "totp": { "code": "<digits>" } } }`.
   - OTP email: PATCH with `{ "challenges": { "otpEmail": {} } }` first, then PATCH again with `{ "checks": { "otpEmail": { "code": "<digits>" } } }`.
   - Passkey (WebAuthn): PATCH with `{ "challenges": { "webAuthN": { "domain": "<our-domain>", "userVerificationRequirement": "USER_VERIFICATION_REQUIREMENT_REQUIRED" } } }`, hand the returned `publicKeyCredentialRequestOptions` to `navigator.credentials.get`, PATCH back with the assertion.

6. **Finalize.** `POST /v2/oidc/auth_requests/{authRequestId}` with `{ "session": { "sessionId": "<id>", "sessionToken": "<token>" } }`. Response contains a `callbackUrl` — an absolute URL on the Zitadel instance like `https://<instance>.zitadel.cloud/oauth/v2/authorize/callback?…`.

7. **Navigate.** `window.location.href = callbackUrl`. Zitadel completes the auth code issuance and 302s the browser back to `<base>/auth/callback`. The existing zitadel-go middleware in `apiauth.go` picks it up, exchanges the code, sets the cookie, and lands the user on whatever URL they were trying to reach. No changes to `apiauth.go` for this leg.

### What does *not* change

- `internal/auth/apiauth/apiauth.go` — OIDC code flow, cookie middleware, `/auth/callback` exchange. Untouched.
- `RedirectURI`, `PostLogoutURI`, and the OIDC scopes in `internal/command/server.go:182-188`. Untouched.
- The `/auth/token` endpoint and `webui/src/lib/transport.ts`. Untouched — app JWTs and org switching work the same way.
- The "remember me" cookie behavior. The session that the cookie middleware sets is unrelated to the Zitadel session created in step 3 of the login flow; the cookie outlives the Zitadel session and gates everything in `/ui/*` after login.

### Configuration changes

Two new server-side knobs in `apiauth.Config`, populated from new CLI flags / env vars in `internal/command/server.go`:

- `LoginClientToken` (PAT for the `IAM_LOGIN_CLIENT`-roled service account). Required to talk to Session API and OIDC v2 API.
- `LoginUIBaseURL` (typically `<baseURL>/ui/login`). Used only for the operator-facing log line at startup that says "set this as the Custom Login UI URL on your Zitadel application." The actual configuration on the Zitadel side is a one-time admin-console action, not something xagent does on boot.

A `webui/.env` (or build-time constant) does not need a new entry — the proxy URL is same-origin.

## Trade-offs

**Server-side proxy vs. browser-direct to Zitadel.** The Session API requires a PAT for a user with the `IAM_LOGIN_CLIENT` role — that's a privileged credential. Letting the browser hold it would broadcast it to anyone hitting the login page. The proxy keeps it on the server and gives us a place to add rate limiting, logging, and pre-flight validation later if we want.

**Reference vs. fork of `zitadel/typescript`.** The TS app is permissively licensed and would be a fine starting point, but it's a full Next.js application with its own state management, i18n, theming, and routing layered on top of the Session API plumbing. Reading it to understand the flow, then writing our own React components against the same APIs, is less code than carrying a fork — and it's the only path that delivers the autofill fix without inheriting whatever caused the bug in upstream's form structure.

**One-shot vs. step-by-step UI.** The flow above is structured as discrete steps (username → password → MFA), which mirrors what users see on most login pages today. A simpler "all on one screen" form is possible since the Session API accepts username and password checks in a single `POST /v2/sessions` (`checks.user.loginName` + `checks.password.password` together). The step-by-step shape is preferred because (a) it surfaces "unknown user" before asking for a password, (b) it matches the prefill story for external IDPs, and (c) it lets us render the username-step `loginName` as a hidden input in the password step, which is the autofill fix.

**Where the Zitadel session token lives.** It lives in React component state for the duration of the login flow and never touches local storage. That is fine: the session token is short-lived, single-purpose, and ceases to matter as soon as `POST /v2/oidc/auth_requests/{id}` is called. If the user reloads mid-flow they restart from the username step, which is acceptable behavior — same as today.

## Open Questions

1. **Existing-session shortcut.** A user who already has a valid Zitadel session (e.g. they signed into another app on the same Zitadel instance, or just signed in here and the OIDC `prompt` doesn't say `login`) can skip the form entirely — the auth request can be finalized against the existing session. Detecting that requires `ListSessions` or similar; it's a nice-to-have for v1 since most users will hit the login page only when their cookie has expired and there's no live Zitadel session either. Worth scoping in v2.

2. **External IDPs.** If the Zitadel application has Google/GitHub/etc. configured as login methods, the auth-request response lists them. Adding "Sign in with X" buttons that initiate the external-IDP redirect dance is a separate, contained piece of work; v1 can omit it and gracefully fall back to the username/password form, with the caveat that anyone relying on external IDPs would be blocked until v2 lands. Confirm whether any current xagent users use an external IDP — if not, deferring is safe.

3. **Self-service.** Zitadel's hosted UI exposes registration, password reset, and email verification flows. xagent currently relies on those for first-time setup. Our custom login does not need to reimplement all of them — for `forgot password` we can link to Zitadel's hosted reset flow at `<instance>.zitadel.cloud/ui/v2/password/reset` and reload back to our login on completion. Acceptable for v1; revisit if the password manager problem turns out to also affect those pages.

4. **Where the per-step state lives in TanStack Router.** Search params (`?step=password`) survive reload but expose flow state in URLs; route-level state is cleaner but loses on reload. Default: search params for `step`, component state for the session token. This is a webui implementation detail to settle during the build.
