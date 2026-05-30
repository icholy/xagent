# Custom Zitadel Login UI

Issue: https://github.com/icholy/xagent/issues/703

## Problem

xagent currently delegates the entire login flow to Zitadel Cloud's hosted Login UI (the V2 login served at `/ui/v2/login` on the managed `*.zitadel.cloud` domain). The OIDC code flow is configured in `internal/auth/apiauth/apiauth.go:159-188` via `github.com/zitadel/zitadel-go/v3/pkg/authentication`, with the redirect URI set to `<base>/auth/callback` in `internal/command/server.go:186`. xagent's own server never sees the username/password form — it only receives the authorization code after Zitadel finishes.

The hosted V2 page does not play well with password managers: 1Password, Bitwarden, and the browser-built-in managers frequently fail to autofill the password field. The user has to type credentials by hand every time the cookie session expires. The managed branding settings expose logos, colors, and message strings — not autocomplete attributes, form layout, or DOM structure — so this can't be fixed from inside the Zitadel admin UI.

Everything else about the current setup is fine: OIDC is correctly wired, MFA works, and the cookie middleware (`a.cookie` in `apiauth.go:124`) is unchanged by anything below. The replacement only needs to take over what the user *sees and types into* — credential collection, MFA prompts, and the redirect back into the OIDC flow.

## Design

Zitadel offers three customization tiers, and only the third can actually fix autofill:

1. **Hosted V2 branding.** Logos, colors, custom domain (`login.acme.com`), and message-string overrides via the Settings V2 API. Does not expose the form HTML. *Not sufficient.*
2. **Self-hosted Login UI (fork of `zitadel/typescript`).** Zitadel ships their own Next.js Login app as open source. We fork it, fix the autocomplete attributes, deploy it ourselves, and point Zitadel at it. We get full HTML/CSS control while keeping Zitadel's session/MFA/passkey logic.
3. **Build our own from scratch against the Session + OIDC v2 APIs.** Maximum control, weeks of work, recreates MFA/passkey/external-IDP flows ourselves.

This proposal picks **Option 2**. Option 1 doesn't solve the problem. Option 3 is many weeks of identity-flow work to fix a CSS/attribute bug.

### Does the Zitadel Cloud free tier support this?

Yes. The relevant capabilities are all documented as available on Cloud instances (not gated to self-hosted or paid tiers):

- **`IAM_LOGIN_CLIENT` role.** Granted in *Default settings → Manager* on the Cloud console to a service account; this is the role the Login app uses to call the Session API on the user's behalf. (See [Login App docs](https://zitadel.com/docs/guides/integrate/login-ui/login-app) and [Self-Hosting Login Client](https://zitadel.com/docs/self-hosting/manage/login-client) — the role itself is instance-level and is not a paid-plan feature.)
- **Service accounts + Personal Access Tokens.** Standard free-tier feature.
- **Trusted Domains.** The login UI's domain (e.g. `login.xagent.choly.ca`) must be added to the instance's Trusted Domains list. Configurable from the Cloud console.
- **Session API + OIDC v2 API.** Both are part of the standard Zitadel API surface available on Cloud — they are what the managed hosted UI itself uses.
- **"Custom login UI URL" on the Application.** Each Zitadel application has a setting to point its OIDC flow at an external login UI instead of the managed `/ui/v2/login`.

The only Cloud-specific concern is that our instance currently lives on a `*.zitadel.cloud` subdomain. The custom Login UI does not require a custom Zitadel domain — it only requires that *our* login domain be in Trusted Domains. The instance keeps its current URL; only the user-facing login form moves.

### Phase 1 — Stand up the forked Login app

1. **Fork `zitadel/typescript`.** Pin to the same minor version as our Zitadel Cloud instance ([Login App: Versioning](https://zitadel.com/docs/guides/integrate/login-ui/login-app)). Live in a sibling repo under `icholy/xagent-login` so it can iterate independently of `xagent`'s release cadence.
2. **Service account + PAT.** In the Cloud console: create a service account `xagent-login-client`, mint a PAT, grant `IAM_LOGIN_CLIENT` under Default settings.
3. **Deployment target.** Two options here, expanded under *Trade-offs*. The default plan is to deploy the Next.js app to Fly.io alongside the existing `xagent` server (we already have a Fly project from `fly.toml`), under `login.xagent.choly.ca`. PAT lives in Fly secrets.
4. **Environment.** Per Zitadel's docs, only three vars are mandatory: `ZITADEL_API_URL=https://<our-instance>.zitadel.cloud`, `ZITADEL_SERVICE_USER_ID`, `ZITADEL_SERVICE_USER_TOKEN`. Optional: `EMAIL_VERIFICATION=true` (we already require verified email today).
5. **Trusted Domain.** Add `login.xagent.choly.ca` in the Zitadel Cloud console.
6. **Application config.** On the xagent application registered in Zitadel, set the "Custom Login UI URL" to `https://login.xagent.choly.ca`. Zitadel's OIDC authorize endpoint will redirect there instead of `/ui/v2/login`.

After Phase 1 the user-visible login looks the same (because we haven't changed any HTML yet), but the form is served from our domain. No changes are needed in `xagent` itself — `apiauth.go` still talks OIDC to the Zitadel instance; only the page the browser hits between `/authorize` and `/auth/callback` has moved.

### Phase 2 — Fix the autofill bug

The actual fix lives in the forked login app, not in xagent. The most likely culprits, based on the typical pattern that breaks password managers:

- **Two-step username → password forms** where the second step is rendered into a different DOM element (or behind a route change) without the username field still present in the form. Password managers heuristically look for a username field paired with the password field; if it's been unmounted, autofill silently misses.
  - Fix: keep a hidden, populated `<input type="text" autocomplete="username" readonly>` in the password-step form, mirroring the username from step 1.
- **`autocomplete` attributes.** Ensure username inputs use `autocomplete="username"` (not `off`, not `email`) and password inputs use `autocomplete="current-password"`.
- **`name` attributes.** Some password managers look at `name="username"` / `name="password"` in addition to `autocomplete`.
- **Form submission via `<button onClick>` instead of a real form submit.** Some managers only save credentials when a `<form>` is submitted; a click-handler-only flow may load fine but never trigger the "save password" prompt.

The fork lets us iterate on these one at a time in a real browser with real password managers, which we cannot do against the hosted page.

### Phase 3 — Reduce drift from upstream

Once the fix is in place, the fork has a long-term maintenance burden: Zitadel's TypeScript login app is under active development and will get security fixes. The fork strategy that minimizes drift:

- Keep the fork as a **thin layer**: only the changed components live in our repo; everything else is upstream.
- Rebase against upstream `main` on a regular schedule (e.g. monthly), driven by a Dependabot-style alert.
- Pin the Zitadel API surface to the version of our Cloud instance, since Cloud lags self-hosted by a release or two.

If upstream eventually fixes the autofill issue, we delete the fork and switch the Application's Custom Login UI URL back to the managed `/ui/v2/login`. This is a one-setting reversal with no code changes in xagent.

### What changes in `xagent` itself

Essentially nothing in the Go code. The OIDC flow in `apiauth.go` continues to point at the Zitadel instance domain, and Zitadel handles the redirect to our custom login UI based on its application config. The webui isn't involved either — the login page is served from a separate domain.

Two small operational changes:
- `internal/command/server.go:182-188`: no flag changes. `Domain`, `ClientID`, `ClientSecret`, `RedirectURI`, `PostLogoutURI` all stay as today.
- README + ops docs: a section describing the login-app deployment, the PAT rotation procedure, and the rollback (flip the Custom Login UI URL back to the managed default).

## Trade-offs

**Fork vs. build from scratch.** Forking inherits the Session/MFA/passkey/external-IDP plumbing for free, which is the bulk of an identity UI. The cost is rebasing against upstream. Building from scratch costs weeks per flow (MFA, passkeys, external IdPs each need their own page), all to land at the same autofill fix. The fork wins until the diff against upstream grows past a couple of components, at which point we'd reconsider.

**Deploy to Fly vs. deploy to Vercel.** Zitadel's docs use Vercel as the example deployment target. Vercel is one click to ship a Next.js app, and gives previews per PR. Against that, we already operate Fly (`fly.toml`), have secrets management there, and don't want a second hosting bill or a second place to check status during an incident. Picking Fly: one fewer vendor, slightly more setup. If the Fly Next.js path turns out to be painful, Vercel is the documented fallback.

**Custom domain on our login UI vs. on Zitadel itself.** This proposal keeps the Zitadel instance on its `*.zitadel.cloud` URL and only puts the login UI on `login.xagent.choly.ca`. An alternative is to put Zitadel itself behind `auth.xagent.choly.ca`, which makes the OIDC issuer match our brand. That is a Cloud "Custom Domain" feature, available on Cloud, but unrelated to fixing autofill — it's a separate concern that can be done later or never. Out of scope here.

**Status of the existing Login UI on free tier.** Zitadel Cloud has historically restricted some enterprise features to paid plans. Before committing engineering time we should confirm in the Cloud console that *(a)* the `IAM_LOGIN_CLIENT` role is grantable on our instance and *(b)* the application has a "Custom Login UI URL" field. Both are documented as Cloud features, but tier gating is the kind of detail docs sometimes elide. This is the first thing to verify in Phase 1 step 2 — if either is paid-only, the plan changes (see Open questions).

## Open Questions

1. **Free-tier verification.** Before any code is written, log into the Zitadel Cloud console for our instance and confirm: can a service account be granted `IAM_LOGIN_CLIENT`? Does the xagent application show a "Custom Login UI URL" field? Both should be present on the free Cloud plan based on docs — but if either turns out to be paywalled, this proposal needs to switch to Option 3 (build from scratch against the public OIDC v2 / Session APIs, which are not gated) and the effort estimate triples.

2. **Domain.** `login.xagent.choly.ca` assumes the current production deployment. If the production domain is different, swap accordingly. The login app must be on a domain we control, since one of the Trusted Domains entries needs to point at it.

3. **Existing sessions on cut-over.** When the Custom Login UI URL flips, do live cookie sessions survive (they should — the cookie is on xagent's domain, not Zitadel's), and what happens to mid-flight `/authorize` calls? Worth a brief test in a staging app/instance before the production flip.

4. **Where does the fork live?** Sibling repo `icholy/xagent-login` keeps it out of `xagent`'s Go build and release pipeline. A monorepo subdirectory (e.g. `login/`) would be one fewer repo but couples a Next.js app to xagent's CI matrix. Default to sibling repo; revisit if cross-repo coordination becomes painful.
