# SSH debug shell

Issue: https://github.com/icholy/xagent/issues/1142

## Problem

The reverse debug shell (`proposals/draft/driver-reverse-shell.md`, `internal/shell/`)
tunnels an interactive terminal from the driver to the operator over a **custom
binary WebSocket framing** — `xagent-shell.v1`, `[1-byte type][payload]` with
types `data`/`resize`/`exit`/`ping` (`internal/shell/shellwire/shellwire.go`).
The driver allocates a PTY and pipes bytes (`shell.Serve`); the server is a
mode-agnostic byte pump (`shellrelay`); the operator drives its local terminal
(`shell.Attach`).

That gets an interactive prompt and nothing else. The bespoke protocol only ever
does what we build into it, and it re-implements — narrowly — the exact machinery
SSH already standardises (channels, `pty-req`, `window-change`, `exit-status`,
keepalive). An operator debugging a task cannot copy a file out (`scp`/`sftp`),
forward a port to a service inside the sandbox, run a non-interactive command and
capture its exit code, or point VS Code Remote-SSH at the sandbox filesystem.
Each of those is a new frame type plus a new client implementation on **both**
legs — i.e. slowly regrowing a private, half-featured SSH.

We want the same reverse-rendezvous shell — nothing about the egress-only
sandbox / un-dialable operator topology changes — but speaking **SSH**, so file
transfer, port-forwarding, non-interactive `exec`, and editor integration come
for free from the tools operators already have.

## Design

### Principle: SSH is a payload upgrade, not a transport change

The reason the shell tunnels over a server-brokered rendezvous is unchanged by
this proposal: the sandbox is egress-only and the operator's terminal isn't
reachable either, so **both ends dial the server**, which bridges the two byte
streams (`driver-reverse-shell.md`, "the driver is the shell, the server is the
rendezvous"). SSH does not alter that topology — it does not need a listening
port on the sandbox and does not care what carries its bytes. SSH is defined over
"any reliable, flow-controlled, 8-bit clean byte stream" (RFC 4253 §1); the
existing rendezvous is exactly that.

So we keep the **entire rendezvous transport** and swap only the *payload* that
rides it:

- `OpenShell(task_id) -> {session_id, ...}` — unchanged shape.
- `GET /shell/driver?session=<id>` (WebSocket, task-token auth) — unchanged.
- `GET /shell/attach?session=<id>` (WebSocket, Bearer auth, org-scoped) — unchanged.
- The in-memory `shellrelay` registry and the verbatim byte pump — **unchanged**.
  The relay already "never parses frames"; SSH is just different bytes through
  the same pump.

The only wire-level change is the subprotocol token: `xagent-shell.v1` →
`xagent-ssh.v1` (versioning only; see Transport). The custom `data/resize/exit/ping`
framing in `internal/shell/shellwire` is deleted — SSH's own channel messages
carry all four.

```
        before                              after
  operator terminal                   system ssh / scp / sftp
        │  shellwire frames                  │  SSH protocol
   /shell/attach (WS) ──┐             /shell/attach (WS) ──┐
                        ├ byte pump (unchanged)            ├ byte pump (unchanged)
   /shell/driver (WS) ──┘             /shell/driver (WS) ──┘
        │  shellwire frames                  │  SSH protocol
   driver: PTY + /bin/sh              driver: in-process sshd
```

### Driver: run an in-process SSH server over the WS leg

`shell.Serve` stops hand-rolling a PTY pump. It wraps its WebSocket leg as a
`net.Conn` and hands it to an SSH server:

```go
// coder/websocket already provides this (internal/shell/shell.go imports the lib).
nc := websocket.NetConn(ctx, conn, websocket.MessageBinary)
sshConn, chans, reqs, err := ssh.NewServerConn(nc, serverConfig) // golang.org/x/crypto/ssh
```

The driver's SSH server handles, in-process (no `sshd` binary in the image):

- **`session` channel + `pty-req` + `shell`/`exec`** — allocate a PTY with the
  same `github.com/creack/pty` we use today, spawn `$SHELL` (login shell, leading
  `-` in argv[0], exactly as `Serve` does now) for `shell`, or the requested
  command for `exec`. `window-change` requests → `pty.Setsize` (was the `resize`
  frame). The command's exit status is returned as SSH `exit-status` (was the
  `exit` frame). SSH keepalive replaces the `ping` frame.
- **`subsystem sftp`** — run an in-process SFTP server (`github.com/pkg/sftp`,
  `sftp.NewServer` over the channel). This is what makes `scp`, `sftp`, and
  `rsync -e ssh` work — file transfer both directions, for free.
- **`direct-tcpip`** — port-forwarding into the sandbox (`ssh -L`), so an
  operator can reach a service bound inside the sandbox. Optional for v1 (see
  Open Questions) but a channel type SSH gives us rather than a frame we design.

Everything the current `Serve` does becomes a subset of the `session`-channel
handler; the new capabilities are additional channel/subsystem handlers on the
same connection, multiplexed by SSH.

### Operator: the CLI is a stdio SSH tunnel, not a terminal driver

Today `xagent shell` *is* the terminal: it opens `/shell/attach`, sets the local
tty raw, and pumps `shellwire` frames (`shell.Attach`). Under SSH the CLI's job
shrinks to **carrying SSH bytes between the operator's SSH client and the WS
leg** — a `ProxyCommand`-style stdio bridge. This is the whole ecosystem payoff:
the operator's real `ssh` (and therefore `scp`, `sftp`, `rsync`, port-forwarding,
VS Code Remote-SSH) drives the sandbox.

Two entry points:

1. **`xagent shell <task>`** — unchanged UX. Internally: `OpenShell`, dial
   `/shell/attach`, then run an **embedded** SSH client
   (`ssh.NewClientConn` over `websocket.NetConn`) that requests a pty + shell and
   pumps stdin/stdout/resize — behaviourally identical to today's `Attach`, just
   with SSH underneath. No dependency on a system `ssh` binary for the common case.

2. **`xagent shell --stdio <task>`** — the bridge: `OpenShell`, dial
   `/shell/attach`, and copy raw bytes between stdin/stdout and the WS leg. It
   speaks no SSH itself. This is designed to be a `ProxyCommand`:

   ```
   # one-off
   ssh -o ProxyCommand="xagent shell --stdio %n" task-1032

   # persistent, so scp/sftp/rsync/code just work
   Host task-*
       ProxyCommand xagent shell --stdio %n
       User root
   ```

   `xagent shell config` emits that `~/.ssh/config` block (host alias `task-<id>`,
   the `ProxyCommand`, pinned host key — see Auth). With it installed:

   ```
   scp task-1032:/root/build.log .          # pull an artifact
   sftp task-1032                           # browse the fs
   rsync -e ssh -a task-1032:/root/out ./   # sync a tree
   code --remote ssh-remote+task-1032 /root # VS Code Remote-SSH
   ssh -L 8080:localhost:3000 task-1032     # forward a port
   ```

None of these are new server or driver features beyond the channel handlers
above — they are what any SSH server exposes.

### Auth: ephemeral, server-minted keys — no user key management

The rendezvous legs are **already** authenticated and authorized: the attach leg
by Bearer token scoped to the task's org (single-occupancy), the driver leg by
the task token bound to the session (`driver-reverse-shell.md`, Security). That
transport-level gate is unchanged and remains the real access-control boundary.

SSH still requires host + user auth of its own, but we make it **automatic and
ephemeral** so the operator never manages keys. On `OpenShell`, the server mints
a throwaway keypair set for the session and distributes it over the two channels
that are *already* authenticated:

- To the **operator** (in the `OpenShell` response): the session user **private
  key** and the expected **host public key** (for `known_hosts` pinning).
- To the **driver** (via the task, alongside `shell_session`): the **host
  private key** and the authorized operator **public key**.

`OpenShellResponse` and the material carrier grow accordingly:

```proto
message OpenShellResponse {
  string session_id = 1;

  // Ephemeral, single-session SSH material. Minted by the server per OpenShell,
  // delivered only over the already-authenticated Connect call. The operator
  // pins host_public_key in known_hosts and authenticates with user_private_key.
  bytes user_private_key = 2;   // PEM (ed25519)
  bytes host_public_key  = 3;   // authorized_keys line, for known_hosts pinning
}
```

The driver's half rides the same server-owned, driver-read path `shell_session`
already uses. Rather than widen the `Task` proto with secret-bearing fields,
`shell_session` stays the mode selector/rendezvous id and the SSH material is
fetched by the driver from a dedicated, task-token-scoped RPC at fork time:

```proto
// Returns the ephemeral SSH server material for this run's rendezvous. Authed
// with the task token; only returns material for the caller's own task, and
// only while shell_session is set. Never surfaced to operators.
rpc GetShellServerKey(GetShellServerKeyRequest) returns (GetShellServerKeyResponse);

message GetShellServerKeyRequest { int64 task_id = 1; }
message GetShellServerKeyResponse {
  bytes host_private_key       = 1; // PEM (ed25519)
  bytes authorized_user_key    = 2; // operator public key, authorized_keys line
}
```

Keys are ed25519 (small, fast, no parameter choices), generated with
`crypto/ed25519` + `ssh.NewSignerFromKey` / `ssh.MarshalAuthorizedKey`. They live
only for the session: the server holds them in the same in-memory session entry
as the `shellrelay.Session` and drops them on teardown (the existing `onClose`
that already clears `shell_session`). Because the material is per-session and
single-use, and the transport is independently authed, a leaked session id still
buys nothing — consistent with today's "session id is not a secret" stance.

Net UX: `xagent shell` is unchanged; the `--stdio`/`config` path pins a real host
key and presents a real user key without the operator ever running `ssh-keygen`.

### Server: still a byte pump, plus a tiny key broker

The server's bridging role is untouched — it copies binary WebSocket messages
between the two legs verbatim and never parses SSH (it holds no session key it
could decrypt with; SSH is end-to-end between operator and driver). The only
additions are at `OpenShell` time:

- generate the ephemeral keypair set (cheap, ed25519),
- stash it in the session entry beside the existing `shellrelay.Session`,
- return the operator half in `OpenShellResponse`, serve the driver half from
  `GetShellServerKey`,
- discard both on teardown via the existing `onClose` hook.

Session lifecycle, the establishment timeout, single-occupancy attach, and
`ClearShellSession` on teardown are all as in `driver-reverse-shell.md`.

### Runner and data model: unchanged

The runner remains oblivious (lifecycle only). `tasks.shell_session` keeps its
exact role — non-empty ⇒ this sandbox run is a shell for that rendezvous — and
keeps selecting the driver fork in `internal/agent/driver.go`. No migration
beyond what `driver-reverse-shell` already defines; the SSH keys are ephemeral
and never persisted to the tasks table.

### Web terminal

The reverse-shell design kept the browser cheap on purpose: xterm.js speaks the
`data/resize/exit` frames directly, so a web terminal was "just another client"
(`driver-reverse-shell.md`, "Web-FE-later"). SSH complicates that — browsers
cannot speak SSH natively — so this proposal has to say how the browser is served
without stranding it. **The driver speaks only SSH; something on the operator
side terminates it.** For the browser that terminator is a small **server-side
SSH-client bridge**: for a browser attach the server dials the driver leg's SSH
as a client (it already mints the ephemeral user key, so it can), and exposes the
resulting PTY stream to xterm.js over a `data/resize/exit` WebSocket — i.e. the
old simple framing survives *only* as the browser⇆server hop, never on the wire
to the driver.

That does put the server in the SSH path **for browser sessions only** (a
deliberate, scoped exception to "server is a dumb pump"); CLI/tooling sessions
stay a pure byte relay end-to-end. The browser terminal is not built in v1 (it
wasn't in the reverse-shell v1 either) — this section exists to show SSH doesn't
corner it. See Trade-offs for the alternative (in-browser wasm SSH) and why the
server-bridge is preferred.

## Trade-offs

- **SSH vs. the bespoke `xagent-shell.v1` framing.** The custom protocol is
  smaller and, for the single use case of "an interactive prompt for xterm.js,"
  marginally simpler. SSH wins the moment you want anything else: file transfer,
  port-forwarding, non-interactive exec, and editor remote-dev are *existing SSH
  features* reachable with the operator's existing tools, versus one hand-built
  frame type and one hand-built client each under the custom scheme. We are
  otherwise on a path to reimplement SSH badly. Cost: a real protocol
  (`golang.org/x/crypto/ssh`, `github.com/pkg/sftp`) and its handshake overhead
  on each session, and the browser wrinkle below.

- **Server-side SSH-client bridge vs. in-browser wasm SSH for the web terminal.**
  A wasm/JS SSH client would preserve "server is a pure pump" for the browser
  too, but it is a heavy, awkward dependency and puts the ephemeral user key in
  untrusted browser JS. The server-side bridge keeps the key server-side and the
  browser on a trivial `data/resize/exit` socket, at the cost of the server
  parsing SSH for browser sessions only. Since the server already mints the
  session's keys, it is already inside that trust boundary; the bridge adds no
  new secret exposure. CLI sessions are unaffected and stay end-to-end.

- **Ephemeral server-minted keys vs. operator-managed keys / SSH CA.** Reusing
  the operator's own `~/.ssh` key would mean managing `authorized_keys` in every
  sandbox and leak the operator's identity key into the trust decision; a full
  SSH CA (server as CA, short-lived certs) is the "correct at scale" answer but
  is more machinery than a per-session throwaway keypair needs. Ephemeral keys
  delivered over the already-authenticated `OpenShell`/`GetShellServerKey` calls
  give real SSH mutual auth with zero operator setup, and the transport auth
  remains the actual gate. If we later want `scp`-without-`ProxyCommand` or
  multi-tenant host trust, promoting to an SSH CA is a compatible next step.

- **Keeping the WebSocket rendezvous vs. exposing SSH more directly.** One might
  ask for a real SSH port. The topology forbids it (egress-only sandbox,
  un-dialable operator) — the whole reason the rendezvous exists. Tunnelling SSH
  over the existing WS legs reuses all of that (NAT traversal, backend-agnostic,
  the auth gate, single-occupancy) and changes only the bytes, which is why this
  is a low-risk swap rather than a new subsystem.

- **`--stdio` ProxyCommand vs. a local listening SSH proxy.** We could instead
  bind a localhost TCP port and let the user `ssh -p <port> localhost`. A
  `ProxyCommand` stdio bridge needs no port, no lifetime management, and no
  bind-address security questions, and composes directly with `scp`/`sftp`/`code`
  via one `~/.ssh/config` block. A local listener may still be worth adding later
  for tools that can't set a `ProxyCommand`, but it isn't needed for v1.

## Open Questions

- **`direct-tcpip` / port-forwarding in v1?** It is a channel type the driver's
  SSH server can accept cheaply, but forwarding from inside an egress-only
  sandbox to the operator's laptop has its own review (what can be reached, any
  bound-address limits). Ship interactive + `sftp` first and gate `-L`/`-R`
  behind a follow-up, or include it from the start?

- **Non-interactive `exec` authorization.** SSH `exec` lets the operator run one
  command and capture output/exit without a PTY — useful for scripting and for
  `scp`'s underlying `scp -t`. It is strictly less than an interactive shell
  (which already grants arbitrary execution), so it seems free to allow, but
  confirm there's no auditing/`OpenShell`-gating assumption that a session is
  always interactive.

- **Host-key pinning UX for the raw `ssh` path.** `xagent shell config` writes a
  pinned `host_public_key` per session, but the key is per-`OpenShell` and thus
  rotates every session, so a static `known_hosts`/`config` entry will mismatch
  on the next open. Options: `StrictHostKeyChecking accept-new` in the emitted
  block, a per-session `-o` injected by `--stdio`, or a stable per-task host key
  minted once and reused across that task's sessions. Which is least surprising?

- **Session-recording.** The reverse-shell design noted "optional session
  recording — free, since the server is in the byte path." With SSH end-to-end
  the server can no longer see plaintext for CLI sessions. If recording is
  wanted, it must move to the driver (which has the cleartext PTY) or the browser
  bridge. Is auditing "who attached, when" (still available at the transport
  layer) sufficient, or is content recording a requirement?
