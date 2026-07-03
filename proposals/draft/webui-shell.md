# Web UI task shell

Issue: https://github.com/icholy/xagent/issues/1145

## Problem

The driver reverse shell (`proposals/draft/driver-reverse-shell.md`, issue #1110)
gives us a backend-agnostic interactive shell into a task's sandbox: `OpenShell`
seeds a rendezvous session, the driver dials the `/shell/driver` leg, an operator
dials `/shell/attach`, and the server bridges the two byte streams using the
`xagent-shell.v1` framing (`internal/shell/shellwire`).

Today the only operator client is the CLI: `xagent shell <task-id>`
(`internal/command/shell.go`) drives `shell.Attach` (`internal/shell/attach.go`),
which puts the local terminal into raw mode and pipes stdin/stdout over the
WebSocket. Most users live in the web UI (`webui/`), not a terminal. When a task
fails and you want to "look around and see what went wrong", you have to leave the
browser, install and configure the CLI, and authenticate separately.

We want an **in-browser terminal** on the task detail page that attaches to the
task's sandbox, reusing the existing rendezvous machinery unchanged. The browser
raises exactly one problem the CLI never had: `WebSocket` cannot set an
`Authorization: Bearer` header, but `/shell/attach` is gated by `RequireAuth` plus
an org-membership check (`internal/server/shellserver/shellserver.go`
`AttachHandler`). Everything else — the relay, the framing, the driver, the
`OpenShell` RPC — already works and stays as-is.

## Design

### Overview

The browser is just another operator leg. The flow mirrors the CLI:

1. User clicks **Open shell** on the task detail page (`webui/src/routes/tasks.$id.tsx`).
2. The UI calls the existing `openShell` RPC (already generated at
   `webui/src/gen/xagent/v1/xagent-XAgentService_connectquery.ts:68`) with the
   task id and gets back a `session_id`.
3. The UI opens a `WebSocket` to `/shell/attach?session=<id>`, authenticating via
   the app JWT it already holds (see **Authenticating the WebSocket** below).
4. A TypeScript port of the `shellwire` codec frames keystrokes / resizes and
   decodes shell output; an xterm.js terminal renders it.
5. On `Exit` frame or socket close, the terminal shows the exit code and offers a
   reconnect.

No server RPC, relay, or driver changes are required for the happy path. The only
backend change is teaching the attach leg to accept a browser-friendly credential.

### Authenticating the WebSocket

The CLI passes `Authorization: Bearer <app-jwt>` as an HTTP header on the dial
(`internal/shell/attach.go:47`). The browser `WebSocket` constructor exposes no
header API, so we need another channel for the token. Three options were weighed
(see **Trade-offs**); the chosen approach is the **`Sec-WebSocket-Protocol`
subprotocol** carrier, which is the standard workaround and keeps the token out of
URLs (and therefore out of access logs / `Referer`).

The browser offers two subprotocols on the dial:

```ts
new WebSocket(url, ['xagent-shell.v1', `xagent-bearer.${token}`])
```

The server side gains a tiny pre-auth adapter, applied only on the attach route,
that runs *before* `RequireAuth`: if the request carries no `Authorization`
header, it scans the `Sec-WebSocket-Protocol` request header for an
`xagent-bearer.<token>` entry and, if present, synthesizes
`Authorization: Bearer <token>` on the request before delegating. `RequireAuth`
and the existing org-membership check in `AttachHandler` then work unchanged — the
token is a normal org-scoped app JWT with an org claim, so `caller.OrgID` is
populated exactly as it is for the CLI.

```go
// internal/server/shellserver/ (new bearerFromSubprotocol middleware), wired on
// the attach route in internal/server/server.go alongside RequireAuth:
mux.Handle(shell.AttachRoute, alice.New(
    bearerFromSubprotocol,        // NEW: Sec-WebSocket-Protocol -> Authorization
    s.auth.RequireAuth(),
).Then(s.shell.AttachHandler()))
```

The token subprotocol is a valid HTTP token (a JWT is `base64url.base64url.base64url`;
all those characters plus `.` are legal in a subprotocol name). It is **not**
echoed back: `AttachHandler` already calls `websocket.Accept` with
`Subprotocols: []string{shellwire.Subprotocol}` (shellserver.go:220), so the
handshake selects only `xagent-shell.v1` and the bearer entry is silently dropped
from the response. The subprotocol-version check (shellserver.go:229) is unchanged.

The driver leg (`/shell/driver`) is untouched — it authenticates with the task
token as it does today.

### Frame codec in TypeScript

Port `internal/shell/shellwire/shellwire.go` verbatim to
`webui/src/lib/shellwire.ts`. It is small and self-contained: a 1-byte type tag
followed by a payload, over **binary** WebSocket messages.

```ts
// webui/src/lib/shellwire.ts
export const SUBPROTOCOL = 'xagent-shell.v1'
export const READ_LIMIT = 1 << 20 // 1 MiB, matches shellwire.ReadLimit

export const FrameType = { Data: 0x00, Resize: 0x01, Exit: 0x02, Ping: 0x03 } as const

export function encodeData(bytes: Uint8Array): Uint8Array // [0x00, ...bytes]
export function encodeResize(rows: number, cols: number): Uint8Array // [0x01, rows(u16be), cols(u16be)]
export function parse(msg: Uint8Array): { type: number; data?: Uint8Array; rows?: number; cols?: number; code?: number }
```

The browser only ever *sends* `Data` and `Resize` and only ever *receives*
`Data`, `Exit`, and `Ping` (keepalive, ignored) — same asymmetry as `Operate`
(`internal/shell/attach.go:118`). Set `ws.binaryType = 'arraybuffer'`.

### Terminal component

Add [`@xterm/xterm`](https://www.npmjs.com/package/@xterm/xterm) and
`@xterm/addon-fit` to `webui/package.json` (no terminal dependency exists today).
A new component `webui/src/components/task-shell.tsx`:

- Instantiates an xterm `Terminal` + `FitAddon`, mounts it into a container div.
- `term.onData(d => ws.send(encodeData(utf8encode(d))))`.
- Incoming `Data` frames → `term.write(bytes)`.
- On mount and on `FitAddon.fit()` (wired to a `ResizeObserver`), send
  `encodeResize(term.rows, term.cols)` — the browser equivalent of the CLI's
  SIGWINCH handling (`internal/shell/attach.go:74`).
- `Exit` frame → print `\r\n[process exited: <code>]` and disable input, offer a
  **Reconnect** button that re-runs `openShell`.

### UI placement and lifecycle

The shell is a full-height panel reachable from the task detail page. Add a nested
route `webui/src/routes/tasks.$id.shell.tsx` (TanStack file-based routing) so the
terminal has a shareable URL and its own back-navigation; the task detail view
(`tasks.$id.tsx`) renders an **Open shell** button that navigates to it.

Gating and constraints, surfaced in the UI rather than discovered on failure:

- `OpenShell` requires the task to be in a **terminal status**
  (`internal/server/apiserver/shell.go`). Render the button as enabled only for
  those states; otherwise show it disabled with a tooltip explaining the shell
  attaches to a finished task's preserved filesystem.
- The relay allows **one operator per session**. `openShell` while a session for
  the task is already live returns the existing session; a second attach leg is
  rejected by the relay. The UI treats an immediate close after handshake as
  "already attached elsewhere" and shows that message.
- Sandbox boot takes seconds. The attach leg may connect before the driver leg;
  the relay holds it until both are present or the establishment timeout fires
  (`shellrelay.DefaultEstablishTimeout`). Show a "starting sandbox…" state until
  the first byte or a `Ping` arrives.
- When the shell exits, the server clears `shell_session` via the registry
  `OnClose` callback (`internal/server/server.go`, `onShellClose`). No client
  action needed; the reconnect path simply calls `openShell` again.

### What does not change

- Proto: `OpenShell` / `OpenShellRequest` / `OpenShellResponse` and
  `Task.shell_session` are already defined and already generated for the webui.
- Relay, registry, driver, `shellwire` wire format, DB schema.
- The `xagent shell` CLI.

## Trade-offs

**WebSocket auth — why the subprotocol carrier.** Three options:

1. **`Sec-WebSocket-Protocol` subprotocol (chosen).** Token never appears in a
   URL, so it stays out of server access logs, browser history, and `Referer`
   headers. Reuses the existing app-JWT / org-claim path with a ~10-line
   pre-auth adapter and zero changes to `RequireAuth` or the org check. The one
   wart is smuggling a credential through a field meant for protocol negotiation,
   but it is a well-worn pattern (Kubernetes, many WS gateways do this) and the
   token is discarded from the handshake response.
2. **`?token=<jwt>` query parameter.** Simplest to implement, but tokens in URLs
   leak into access logs and proxies; rejected on those grounds even though the
   session id there is already non-secret.
3. **Cookie session.** The server already supports cookie auth for the web UI
   (`internal/auth/apiauth/apiauth.go`), and a same-origin browser WebSocket sends
   the cookie automatically — no token plumbing at all. Rejected because cookie
   callers today resolve to `OrgID = 0` (org is resolved only in the `/auth/token`
   exchange, `HandleToken`), so the org-membership check in `AttachHandler` would
   need a separate org-resolution step and a `?org_id=` param, and it broadens the
   omnipotent cookie-session surface (`User()` grants `authscope.Admin()`) onto a
   raw byte relay. Reusing the narrowly-scoped app JWT the webui already fetches is
   the smaller, better-audited surface.

**xterm.js vs. hand-rolled.** A real VT emulator is required for anything beyond
`echo` (cursor movement, colors, `vim`, `htop`). xterm.js is the de-facto standard,
maintained, and framework-agnostic; hand-rolling is out of the question.

**Nested route vs. modal.** A route gives a shareable/bookmarkable URL and clean
lifecycle (navigating away tears the socket down) at the cost of one more route
file, versus a modal that keeps the user "in place". The route is chosen for the
lifecycle clarity and because a shell is a substantial mode, not a quick dialog.

## Open Questions

- **Reconnect / resumption.** The relay is a stateless byte pump with no scrollback
  replay; a dropped socket loses the session (the driver's shell process dies with
  the leg). Is auto-reconnect-and-reseed acceptable, or do we want the driver to
  keep the PTY alive briefly across an attach-leg drop? Recommend shipping v1 with
  manual reconnect and revisiting if it bites.
- **Idle timeout.** Should the browser send periodic activity, or rely on the
  relay's `Ping` keepalive? Confirm the relay pings frequently enough to keep
  intermediary proxies from idling out the socket.
- **Mobile.** An on-screen terminal is near-unusable on phones; the PWA proposal
  (`proposals/draft/webui-pwa.md`) may want to hide the entry point on small
  viewports rather than render a broken terminal.
- **Audit.** Shell runs already emit the same lifecycle events as agent runs and
  are indistinguishable in the timeline. Do we want to explicitly mark
  browser-opened shell sessions (who attached, when) for auditability?
