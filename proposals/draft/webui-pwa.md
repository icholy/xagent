# Web UI as a Progressive Web App

## Problem

The xagent web UI is a desktop-first React SPA served at `/ui/` by the Go server (`internal/server/static.go` embeds the Vite build via `//go:embed webui` and falls back to `index.html` for client routes). It is usable on mobile browsers but is not installable, has no app icon on the home screen, no splash screen, no offline shell, and — most importantly for an async agent system — no way to notify the user when something they care about happens (a task finishes, fails, posts a comment, or asks a question via an event).

Today, the only way to know a task changed is to keep a tab open. The frontend already has a `NotificationSSE` client (`webui/src/lib/notification-sse.ts`) that subscribes to `/events` and invalidates React Query caches on change notifications, but as soon as the tab is backgrounded on iOS Safari or Chrome Android the connection is torn down and never re-established until the user opens the tab again. The SSE client already contains a `visibilitychange` workaround that explicitly closes and reconnects on resume, which is evidence that mobile lifecycle handling is already a sore spot.

Turning the web UI into a PWA gives us four discrete wins:

1. **Installability** — Add to Home Screen on iOS and Android, runs in a standalone window without browser chrome.
2. **Mobile-first UX scaffolding** — viewport meta is fine but maskable icons, theme colors and a splash screen make it feel like an app instead of a bookmark.
3. **Background event delivery via Web Push** — the only realistic way to notify a user when a task changes while the UI is closed or the device is locked.
4. **Offline shell** — the SPA bundle can be cached so launching the installed app does not show a "no internet" screen when the connection is flaky; live data still requires the server.

## Scope

"PWA" in this proposal means, concretely, the following changes to the existing Vite build:

- **Web App Manifest** at `/ui/manifest.webmanifest` declaring name, short name, theme color, background color, display `standalone`, scope `/ui/`, start URL `/ui/tasks`, and a set of icons (192, 512, maskable 512).
- **Service Worker** registered from `main.tsx`, scoped to `/ui/`. Its responsibilities are kept deliberately narrow:
  - Precache the built JS/CSS/HTML hash-named assets so the app shell loads instantly on launch.
  - Network-first for `index.html` so deploys are picked up.
  - Pass-through (do not intercept) for `/xagent.v1.XAgentService/*`, `/events`, `/auth/*`, and `/webhook/*`. These are live, authenticated, and Connect-RPC framed — the SW must not cache or interfere.
  - Handle `push` and `notificationclick` events for Web Push delivery.
- **App icons & splash** — generate maskable PNGs from the existing `webui/public/icon.png`. iOS-specific `apple-touch-icon` link tags are added to `index.html`.
- **Install prompt** — render the browser's `beforeinstallprompt` (Android/Desktop Chrome) behind a small "Install" affordance in the nav. iOS does not fire this event; document the manual "Share → Add to Home Screen" path in a settings hint.
- **Web Push subscription UI** under Settings → Notifications, with an opt-in toggle that creates a `PushSubscription`, registers it with the server, and lets the user pick which notification types they want.
- **Theme color & viewport meta** updates in `webui/index.html` so the standalone window chrome and status bar match the app.

What is **explicitly out of scope**:

- Offline write/queueing of mutations. xagent is fundamentally a live system: creating a task, restarting a task, replying to an event all require the server. We do not attempt to queue these for later replay.
- Caching of API responses in the service worker. React Query is already the cache layer; duplicating it in the SW would create stale-data bugs.
- Background sync for the SSE stream. SSE inside a service worker is not viable (no `EventSource` in worker context, and `fetch` streams die when the SW is terminated). Real-time stays in the foreground page; backgrounded delivery uses Web Push.

## Real-time strategy under PWA lifecycle

The current SSE client (`webui/src/lib/notification-sse.ts`) already handles three of the four hard cases an installed PWA hits:

- It explicitly **closes the connection on `visibilityState !== "visible"`** and reconnects on resume, because the native `EventSource` ends up in a half-dead state when mobile browsers throttle backgrounded tabs.
- It **invalidates all React Query caches on reconnect** (`useOrgSSE` in `webui/src/hooks/use-org-sse.ts` registers a reconnect listener that calls `queryClient.invalidateQueries()`), so any notifications missed while disconnected are recovered by refetching.
- It uses **exponential backoff** capped at 30s for reconnect attempts.

The cases that change under PWA install:

| Scenario | Current behavior | Under PWA |
| --- | --- | --- |
| Tab visible, foreground | SSE open, reactive updates work | Unchanged |
| Tab backgrounded, browser running | SSE torn down by visibilitychange handler, reconnects on resume | Unchanged. Standalone window has the same `visibilitychange` semantics. |
| App closed entirely | No connection. User sees nothing happened until they reopen. | Web Push delivers a notification via the SW. Tapping it opens the task. |
| OS suspends the app | SSE socket eventually dies; reconnect happens when the user brings the app back to the foreground. React Query refetches on reconnect cover missed state. | Same. Plus any Web Push notifications that arrived while suspended are visible in the system tray. |

So the runtime model for the PWA is **foreground = live SSE, background = Web Push**. SSE does not need to move into the service worker. The SW is purely a delivery channel for push and a cache for the static shell.

### Reconnect on resume

The existing `handleVisibilityChange` close-on-hide / reconnect-on-show flow is reused unchanged. One adjustment: when the SW receives a push that indicates a task this user is watching has changed, the page (if open) can be notified via `postMessage` from the SW so the foreground SSE-driven cache can invalidate immediately even if SSE itself happened to be in a backoff window. This is a small enhancement, not load-bearing.

## Web Push

Web Push is the headline feature; the manifest and SW are mostly scaffolding for it.

### Server side

A new package `internal/webpush/` with three responsibilities:

1. Store `PushSubscription` records (`endpoint`, `p256dh`, `auth`, `user_id`, `org_id`, `created_at`, optional `task_filter`/`event_filter` JSON). New migration in `internal/store/sql/migrations/` and queries in `internal/store/sql/queries/push.sql`.
2. Sign and send pushes using VAPID. The Go ecosystem has `github.com/SherClockHolmes/webpush-go` (small, no cgo, used widely). VAPID keys are generated once and stored as server flags: `XAGENT_VAPID_PUBLIC_KEY`, `XAGENT_VAPID_PRIVATE_KEY`, `XAGENT_VAPID_SUBJECT` (a `mailto:` URL).
3. Hook into the existing notification pipeline. The cleanest place is `internal/server/notifyserver/` — alongside the SSE fanout, an outbound goroutine watches notifications for the same org and dispatches Web Push to any matching subscriptions whose filter allows it. The match logic is the same scope already used for SSE (org + resource type) so we are not building a new routing layer.

Three new Connect RPCs in `proto/xagent/v1/xagent.proto`:

```
rpc GetWebPushConfig(GetWebPushConfigRequest) returns (GetWebPushConfigResponse);
rpc CreatePushSubscription(CreatePushSubscriptionRequest) returns (CreatePushSubscriptionResponse);
rpc DeletePushSubscription(DeletePushSubscriptionRequest) returns (DeletePushSubscriptionResponse);
```

`GetWebPushConfig` returns the VAPID public key so the frontend can call `pushManager.subscribe({ applicationServerKey })` without hardcoding it.

Subscriptions that return 404/410 from the push provider are deleted by the server on next attempt (standard housekeeping).

### Client side

In `webui/src/lib/push.ts` (new):

- A `enablePush()` helper that requests notification permission, calls `navigator.serviceWorker.ready`, calls `pushManager.subscribe`, and POSTs the resulting subscription to `CreatePushSubscription` via the existing Connect transport.
- A `disablePush()` helper that unsubscribes locally and calls `DeletePushSubscription`.
- A `usePushSubscription()` hook for the settings UI that exposes current state (`unsupported | denied | unsubscribed | subscribed`) and toggle handlers.

In the service worker (`webui/src/sw.ts`):

```ts
self.addEventListener('push', (event) => {
  const data = event.data?.json() ?? {}
  event.waitUntil(self.registration.showNotification(data.title ?? 'xagent', {
    body: data.body,
    icon: '/ui/icon.png',
    badge: '/ui/badge.png',
    tag: data.tag,           // e.g. `task-${id}` so repeat updates replace prior toast
    data: { url: data.url },
  }))
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const url = event.notification.data?.url ?? '/ui/tasks'
  event.waitUntil((async () => {
    const all = await self.clients.matchAll({ type: 'window' })
    const existing = all.find(c => c.url.includes('/ui/'))
    if (existing) {
      existing.focus()
      existing.navigate(url)
    } else {
      self.clients.openWindow(url)
    }
  })())
})
```

A settings panel under `webui/src/routes/settings.tsx` (new tab "Notifications") provides:

- Toggle to enable push.
- Per-type filter checkboxes (task completed, task failed, link/event on a task I created, child task finished). These map to the `task_filter`/`event_filter` JSON stored server-side.
- Test button that calls a `SendTestPush` RPC.

## iOS vs Android

**iOS 16.4+** (March 2023) supports Web Push **only for installed PWAs** — Safari tab pages cannot subscribe. The implication is that the install affordance is not a nice-to-have on iOS; it is a prerequisite to push working at all. The settings panel must detect this case (no `PushManager` in tab context on iOS) and show "Install to home screen to enable push" instead of a permission prompt that will silently fail.

Other iOS quirks worth coding around:

- iOS does not fire `beforeinstallprompt`. The install hint is text-only.
- iOS PWAs use `apple-touch-icon` link tags (not just the manifest). Add 180×180 and 167×167.
- `apple-mobile-web-app-status-bar-style` and `apple-mobile-web-app-capable` meta tags shape the standalone status bar.
- iOS PWAs were notoriously aggressive about evicting installed apps when low on storage in older versions; iOS 17 reduced this. Not actionable but worth documenting.
- The notification badge count on the app icon is supported via `navigator.setAppBadge()` on iOS 16.4+. Worth wiring up since the data (unread task changes) is cheap to compute.

**Android Chrome** is the easy path: manifest + SW + VAPID + permission prompt all work as-documented. `beforeinstallprompt` gives us an in-app "Install" button. WebAPK installation has full system integration.

**Desktop Chrome/Edge** also gets installable PWA + Web Push. Firefox supports Web Push on desktop but not "installable PWA" in the same sense — manifest still applies for the appearance fields but there is no standalone window mode.

## Implementation sketch

Frontend changes (build-time):

- Add `vite-plugin-pwa` to `webui/package.json`. It generates the manifest, the service worker (Workbox-backed), the type definitions, and the registration helper. The alternative is hand-rolling all of this; `vite-plugin-pwa` is the standard and saves a lot of boilerplate around precache-manifest injection.
- Configure it in `webui/vite.config.ts` with `registerType: 'autoUpdate'`, `scope: '/ui/'`, `start_url: '/ui/tasks'`, `base: '/ui/'`, and the precache glob restricted to hashed JS/CSS plus `index.html`. Network paths that must not be intercepted are explicitly excluded via `navigateFallbackDenylist` (`/xagent.v1.*`, `/events`, `/auth/.*`, `/webhook/.*`).
- Add maskable icon assets to `webui/public/`. The existing `icon.png` is the source; new sizes are generated as build artifacts.
- Wire the SW registration in `webui/src/main.tsx` behind `import.meta.env.PROD` to avoid SW interference during `vite dev` (which is its own can of worms — dev server HMR and SW caching fight constantly).
- Update `webui/index.html` head with the manifest link, theme color, viewport-fit cover, apple-touch-icons, and apple status bar meta.
- New `webui/src/lib/push.ts` and notification settings UI in `settings.tsx`.

Backend changes:

- `internal/webpush/` package using `webpush-go`.
- New migration adding `push_subscriptions` table.
- Three new RPCs and their handlers in `internal/server/apiserver/`.
- Integration point in `internal/server/notifyserver/` (or a sibling goroutine subscribed to the same pubsub) that dispatches push for relevant notifications.
- Three new server flags for VAPID config; server is a no-op for push if they are unset (same conditional pattern as the GitHub/Atlassian flags today).

Build pipeline impact:

- The Vite build output grows by the SW file (~10–30 KB) and one extra `manifest.webmanifest`. Both end up in `internal/server/webui/` and are picked up by the existing `//go:embed webui` in `static.go` without code changes.
- One subtlety: the SW must be served from `/ui/sw.js` with `Content-Type: application/javascript` and **without** an aggressive `Cache-Control`. The current `http.FileServer` in `static.go` does the right thing for content type via mime sniffing, but we should set `Cache-Control: no-cache` on `sw.js` (and `index.html`) so updated deploys are picked up quickly. Small edit to `static.go`.
- The SW's scope must match the URL it is served from, so it has to live at `/ui/sw.js`. `vite-plugin-pwa` handles this when `base: '/ui/'` is configured.

Rough effort:

- Manifest + SW + icons + install hint + cache-control tweaks: ~1 day.
- Web Push end-to-end (server package, migration, RPCs, frontend opt-in UI, notification routing hook, test push button): ~3–5 days.
- iOS-specific install hint and badging polish: ~half a day.

Total: roughly a week of focused work for a single engineer, dominated by the Web Push plumbing, not the PWA scaffolding.

## Open questions / tradeoffs

1. **VAPID key rotation.** Web Push subscriptions are tied to the VAPID public key the browser saw at subscribe time. Rotating the key invalidates every existing subscription. We need to decide if we ever rotate (and document it as a known cost) or treat the key as effectively permanent and store it in a secret manager.

2. **Per-user vs per-org subscriptions.** A user with access to multiple orgs probably wants push for events in any of them, not just the currently selected org in the UI. The subscription record should be per-user and the dispatch logic should filter by "user has access to the org that emitted the notification" rather than the user's currently active org. This is a different shape than the current SSE-by-org model and needs a tiny bit of care.

3. **Notification filter granularity.** Sending a push for every notification the SSE stream emits would be way too noisy (file changes, log lines, status transitions). The proposed filter (task completed / failed / awaiting input / link added by another user) is a starting set, not load-bearing — we should ship with conservative defaults and iterate. Worth deciding up front whether filter rules live in JSON or get their own proto schema.

4. **Self-notifications.** SSE already filters out notifications originating from the same `client_id` (`notification-sse.ts` line 154). The push dispatcher should similarly skip notifications whose acting user matches the subscription user, so users don't get pushed about their own actions.

5. **Service worker update cadence.** `registerType: 'autoUpdate'` triggers a reload window when a new SW takes control. For a long-lived installed PWA, this can disrupt a session mid-task. The alternative is `prompt` where the UI surfaces a "new version available — reload" toast. Probably ship with `prompt`; it is more polite.

6. **What about the dev server?** `vite-plugin-pwa` has a `devOptions.enabled` flag that registers a stub SW in dev. We should leave it off — SW + HMR causes more confusion than it saves — and rely on `pnpm preview` / a built bundle for any SW debugging.

7. **Privacy of push payload.** Web Push payloads are encrypted client-side with the subscription's `p256dh`/`auth` keys, so they are end-to-end encrypted between server and SW. We can put readable strings ("Task #123 completed") in the body without worrying about the push provider seeing it. Worth noting because some teams pick "data-only push + fetch on receipt" for privacy reasons that don't apply here.

8. **Badge count source of truth.** `setAppBadge(n)` needs a number. Computing "unread task changes" requires a per-user notion of "last seen", which we do not currently track. We can ship the PWA without badging and add it later, or stub badging as "1 if there are any active tasks in a non-terminal state from the last 24h" as a poor-man's approximation. Defer.

## Alternatives considered

**Status quo (mobile browser tab, no install).** Cheapest. Cost: no background notifications, no home-screen presence, no recovery from the SSE-on-mobile pain we already have hand-rolled workarounds for. Rejected because the lack of background notifications is the actual user-facing pain point — async agents producing results the user never sees defeats the value proposition.

**Capacitor wrapper.** Take the existing Vite build and wrap it as a Capacitor app for iOS/Android. Gives us native push (APNs/FCM) instead of Web Push, App Store distribution, and easier deep linking. Costs: an Apple Developer Program membership ($99/year), App Store review cycle, two more build pipelines, native code for push registration. Rejected because the wins over PWA + Web Push are marginal (notification reliability is comparable on modern iOS) and the distribution overhead is large for a tool with a small user base.

**Native app (React Native or Swift/Kotlin).** Best mobile UX. Worst engineering ROI given xagent's scope and team size. Rejected.

**Email / Slack / Pushover for notifications instead of Web Push.** xagent already has Slack and email-adjacent integration shapes (GitHub comments, Jira comments). We could "notify" by posting to a Slack channel the user is in. This is genuinely fine for some users and worth considering as an additional channel, but it is not a substitute for a notification that opens directly to the task that changed — that is what an installed PWA + Web Push gives.

**Browser desktop notifications without PWA installation.** Possible on desktop without any SW or manifest changes via the `Notification` API. Works only while a tab is open, so it solves nothing for mobile and doesn't address the "tab gets discarded" problem on desktop. Not worth building as a separate path; just do the SW route and it covers desktop too.
