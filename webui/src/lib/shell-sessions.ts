import { createClient, type Client, type Transport } from '@connectrpc/connect'
import { XAgentService } from '@/gen/xagent/v1/xagent_pb'
import {
  SUBPROTOCOL,
  FrameType,
  encodeData,
  encodeResize,
  parse,
  type Frame,
} from '@/lib/shellwire'

// ShellSessions owns the browser-side lifecycle of a task's debug shell — the
// OpenShell RPC, the attach WebSocket, and the decoded output stream — OUTSIDE
// the React tree, alongside AuthTransport and NotificationSSE. Components read
// its state via useSyncExternalStore and register interest with attach/detach.
//
// Why this exists: the attach socket used to be owned by a component effect, so
// React StrictMode's dev mount→cleanup→mount double-invocation opened socket A,
// closed it, then opened socket B — both to the same session. The relay tears a
// session down when any operator leg drops, so B landed on a dead session and
// reported "already attached". Moving socket ownership here, keyed per task with
// ref-counted, grace-delayed teardown, means attach→detach→attach nets exactly
// one live socket.

// ShellPhase is the client-visible state of a task's shell.
//   idle       — nothing opened yet (or after teardown)
//   opening    — OpenShell RPC in flight, minting a session
//   starting   — attach socket open, waiting for the sandbox's first byte
//   connected  — the shell is live
//   exited     — the shell process exited (exitCode carries the status)
//   detached   — the socket closed before any byte arrived, which the relay does
//                when a session already has an operator attached
//   error      — the RPC failed, or the socket errored / dropped mid-session
export type ShellPhase =
  | 'idle'
  | 'opening'
  | 'starting'
  | 'connected'
  | 'exited'
  | 'detached'
  | 'error'

export interface ShellState {
  phase: ShellPhase
  exitCode: number | null
  // error holds the OpenShell RPC failure message, surfaced by the shell page's
  // first-open error UI. Socket-level failures use the 'error'/'detached' phases.
  error: string | null
  // started flips true once a session has been minted at least once in this
  // lifecycle and stays true through exited/error/reconnect until teardown. The
  // shell page uses it to decide between its idle/opening chrome and the terminal.
  started: boolean
}

// IDLE is the shared snapshot returned for any task with no live entry. It is a
// module constant so useSyncExternalStore sees a stable reference.
const IDLE: ShellState = { phase: 'idle', exitCode: null, error: null, started: false }

// MAX_SCROLLBACK caps the retained output (bytes) replayed to a freshly mounted
// terminal, so a long-lived session can't grow the buffer without bound.
const MAX_SCROLLBACK = 512 * 1024

// DEFAULT_GRACE_MS is how long teardown is deferred after the last detach. It
// only has to outlast React's synchronous StrictMode remount; a subsequent
// attach cancels it. Kept short so navigating away frees the socket promptly.
const DEFAULT_GRACE_MS = 250

interface Entry {
  orgId: string
  snapshot: ShellState
  sessionId: string | null
  ws: WebSocket | null
  received: boolean
  gotExit: boolean
  refcount: number
  teardownTimer: ReturnType<typeof setTimeout> | null
  lastRows: number | null
  lastCols: number | null
  scrollback: Uint8Array[]
  scrollbackBytes: number
}

// attachURL builds the same-origin ws(s) URL for the operator attach leg. The
// browser cannot set an Authorization header on a WebSocket, so it carries only
// the session and the active org; the cookie session authenticates the handshake.
function attachURL(session: string, orgId: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params = new URLSearchParams({ session, org_id: orgId })
  return `${proto}//${window.location.host}/shell/attach?${params.toString()}`
}

function exitMarker(code: number): Uint8Array {
  return new TextEncoder().encode(`\r\n\x1b[90m[process exited: ${code}]\x1b[0m\r\n`)
}

export interface ShellSessionsOptions {
  client: Client<typeof XAgentService>
  graceMs?: number
}

export class ShellSessions {
  private readonly client: Client<typeof XAgentService>
  private readonly graceMs: number
  private readonly entries = new Map<string, Entry>()
  private readonly stateListeners = new Map<string, Set<() => void>>()
  private readonly outputListeners = new Map<string, Set<(bytes: Uint8Array) => void>>()

  constructor(opts: ShellSessionsOptions) {
    this.client = opts.client
    this.graceMs = opts.graceMs ?? DEFAULT_GRACE_MS
  }

  // fromTransport is the production constructor: it builds the XAgentService
  // client from the app's Connect transport (which carries the Bearer token).
  static fromTransport(transport: Transport): ShellSessions {
    return new ShellSessions({ client: createClient(XAgentService, transport) })
  }

  // attach registers interest in a task's shell and cancels any pending teardown.
  // It does not open a socket — open() does. Pair every attach with a detach.
  attach(key: string, orgId: string): void {
    const e = this.ensure(key, orgId)
    if (e.teardownTimer !== null) {
      clearTimeout(e.teardownTimer)
      e.teardownTimer = null
    }
    e.refcount++
  }

  // detach drops one interest. When the last one goes, teardown is deferred by
  // graceMs rather than run immediately, so a StrictMode remount (which detaches
  // then re-attaches synchronously) keeps the socket alive.
  detach(key: string): void {
    const e = this.entries.get(key)
    if (!e) return
    e.refcount = Math.max(0, e.refcount - 1)
    if (e.refcount === 0 && e.teardownTimer === null) {
      e.teardownTimer = setTimeout(() => this.teardown(key), this.graceMs)
    }
  }

  // open mints a fresh session via OpenShell and connects the attach socket.
  // Concurrent/duplicate calls are deduped: it is a no-op while a session is
  // already opening or live. From a terminal phase (exited/error/detached) it
  // relaunches — the relay has no scrollback, so reconnect is always fresh.
  open(key: string, orgId: string): void {
    const e = this.ensure(key, orgId)
    const phase = e.snapshot.phase
    if (phase === 'opening' || phase === 'starting' || phase === 'connected') return
    this.closeSocket(e)
    e.received = false
    e.gotExit = false
    e.sessionId = null
    e.scrollback = []
    e.scrollbackBytes = 0
    this.update(key, e, { phase: 'opening', exitCode: null, error: null })
    this.client.openShell({ taskId: BigInt(key) }).then(
      (resp) => {
        // Ignore a resolution that races an intervening teardown/reopen.
        if (this.entries.get(key) !== e) return
        this.update(key, e, { started: true })
        this.connect(key, e, resp.sessionId)
      },
      (err: unknown) => {
        if (this.entries.get(key) !== e) return
        const message = err instanceof Error ? err.message : 'failed to open shell'
        this.update(key, e, { phase: 'error', error: message })
      },
    )
  }

  // sendData forwards operator keystrokes as a Data frame (dropped if not open).
  sendData(key: string, bytes: Uint8Array): void {
    const ws = this.entries.get(key)?.ws
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(encodeData(bytes))
    }
  }

  // sendResize records the terminal size and, once the shell is live, forwards it
  // as a Resize frame. Sizes set before the first byte are flushed on connect, so
  // the sandbox PTY comes up with the operator's real dimensions.
  sendResize(key: string, rows: number, cols: number): void {
    const e = this.entries.get(key)
    if (!e) return
    e.lastRows = rows
    e.lastCols = cols
    if (e.received && e.ws && e.ws.readyState === WebSocket.OPEN) {
      e.ws.send(encodeResize(rows, cols))
    }
  }

  // getSnapshot returns the current state for useSyncExternalStore; a task with
  // no live entry reads the shared IDLE snapshot.
  getSnapshot(key: string): ShellState {
    return this.entries.get(key)?.snapshot ?? IDLE
  }

  // subscribe registers a state-change listener for a task (useSyncExternalStore).
  subscribe(key: string, listener: () => void): () => void {
    let set = this.stateListeners.get(key)
    if (!set) {
      set = new Set()
      this.stateListeners.set(key, set)
    }
    set.add(listener)
    return () => {
      const s = this.stateListeners.get(key)
      if (!s) return
      s.delete(listener)
      if (s.size === 0) this.stateListeners.delete(key)
    }
  }

  // subscribeOutput registers an output-frame listener and immediately replays
  // the retained scrollback, so a freshly mounted terminal re-renders what the
  // session has already produced.
  subscribeOutput(key: string, listener: (bytes: Uint8Array) => void): () => void {
    let set = this.outputListeners.get(key)
    if (!set) {
      set = new Set()
      this.outputListeners.set(key, set)
    }
    set.add(listener)
    const e = this.entries.get(key)
    if (e) {
      for (const chunk of e.scrollback) listener(chunk)
    }
    return () => {
      const s = this.outputListeners.get(key)
      if (!s) return
      s.delete(listener)
      if (s.size === 0) this.outputListeners.delete(key)
    }
  }

  // has reports whether a task currently has a live entry. Exposed for tests.
  has(key: string): boolean {
    return this.entries.has(key)
  }

  private ensure(key: string, orgId: string): Entry {
    let e = this.entries.get(key)
    if (!e) {
      e = {
        orgId,
        snapshot: IDLE,
        sessionId: null,
        ws: null,
        received: false,
        gotExit: false,
        refcount: 0,
        teardownTimer: null,
        lastRows: null,
        lastCols: null,
        scrollback: [],
        scrollbackBytes: 0,
      }
      this.entries.set(key, e)
    }
    return e
  }

  private connect(key: string, e: Entry, sessionId: string): void {
    e.sessionId = sessionId
    e.received = false
    e.gotExit = false
    const ws = new WebSocket(attachURL(sessionId, e.orgId), [SUBPROTOCOL])
    ws.binaryType = 'arraybuffer'
    e.ws = ws
    this.update(key, e, { phase: 'starting' })

    ws.onmessage = (ev) => {
      let frame: Frame
      try {
        frame = parse(new Uint8Array(ev.data as ArrayBuffer))
      } catch {
        return
      }
      if (!e.received) {
        // First byte (data or keepalive) means the driver leg is up and live.
        e.received = true
        this.update(key, e, { phase: 'connected' })
        this.flushResize(e)
      }
      switch (frame.type) {
        case FrameType.Data:
          if (frame.data) this.emitOutput(key, e, frame.data)
          break
        case FrameType.Exit: {
          const code = frame.code ?? 0
          e.gotExit = true
          this.emitOutput(key, e, exitMarker(code))
          this.update(key, e, { phase: 'exited', exitCode: code })
          this.closeSocket(e)
          break
        }
        case FrameType.Ping:
          break
      }
    }
    ws.onerror = () => {
      if (e.ws !== ws || e.gotExit) return
      this.update(key, e, { phase: 'error' })
    }
    ws.onclose = () => {
      if (e.ws !== ws || e.gotExit) return
      e.ws = null
      // A close with no bytes ever received is the relay rejecting a second
      // operator; a close after the session was live is an unexpected drop.
      this.update(key, e, { phase: e.received ? 'error' : 'detached' })
    }
  }

  private flushResize(e: Entry): void {
    if (e.lastRows !== null && e.lastCols !== null && e.ws && e.ws.readyState === WebSocket.OPEN) {
      e.ws.send(encodeResize(e.lastRows, e.lastCols))
    }
  }

  private emitOutput(key: string, e: Entry, bytes: Uint8Array): void {
    e.scrollback.push(bytes)
    e.scrollbackBytes += bytes.length
    while (e.scrollbackBytes > MAX_SCROLLBACK && e.scrollback.length > 1) {
      const dropped = e.scrollback.shift()!
      e.scrollbackBytes -= dropped.length
    }
    const set = this.outputListeners.get(key)
    if (set) {
      for (const listener of set) listener(bytes)
    }
  }

  private closeSocket(e: Entry): void {
    const ws = e.ws
    if (!ws) return
    e.ws = null
    // Drop the handlers before closing so this teardown-triggered close can't
    // flip the phase.
    ws.onmessage = null
    ws.onerror = null
    ws.onclose = null
    try {
      ws.close()
    } catch {
      // ignore: closing an already-closing/closed socket is fine
    }
  }

  private teardown(key: string): void {
    const e = this.entries.get(key)
    if (!e) return
    e.teardownTimer = null
    // A re-attach may have arrived after the timer fired but before this ran.
    if (e.refcount > 0) return
    this.closeSocket(e)
    this.entries.delete(key)
    // Wake subscribers (if any) so they re-read the now-IDLE snapshot.
    this.notifyState(key)
  }

  private update(key: string, e: Entry, patch: Partial<ShellState>): void {
    e.snapshot = { ...e.snapshot, ...patch }
    this.notifyState(key)
  }

  private notifyState(key: string): void {
    const set = this.stateListeners.get(key)
    if (set) {
      for (const listener of set) listener()
    }
  }
}
