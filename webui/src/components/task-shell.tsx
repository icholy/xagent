import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { Button } from '@/components/ui/button'
import { Loader2 } from 'lucide-react'
import { SUBPROTOCOL, FrameType, encodeData, encodeResize, parse } from '@/lib/shellwire'

// Phase drives the status overlay layered over the terminal.
//   starting   — socket open, waiting for the sandbox's first byte/ping
//   connected  — the shell is live; no overlay
//   exited     — the shell process exited (code carries the status)
//   detached   — the socket closed before the shell produced anything, which the
//                relay does when a session already has an operator attached
//   error      — the socket errored or closed unexpectedly mid-session
type Phase = 'starting' | 'connected' | 'exited' | 'detached' | 'error'

interface TaskShellProps {
  // session is the rendezvous id minted by OpenShell. Changing it re-establishes
  // the socket, which is how the reconnect path (a fresh OpenShell) works.
  session: string
  // orgId is the active org, passed as the org_id query param so the cookie-
  // authenticated attach leg can resolve membership (see the SSE stream).
  orgId: string
  // onReconnect asks the parent to mint a new session (OpenShell again). The
  // relay is a stateless byte pump with no scrollback replay, so a reconnect is
  // always a fresh session, never a resume.
  onReconnect: () => void
  reconnecting?: boolean
}

// attachURL builds the same-origin ws(s) URL for the operator attach leg. The
// browser cannot set an Authorization header on a WebSocket, so it carries only
// the session and the active org; the cookie session authenticates the handshake.
function attachURL(session: string, orgId: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params = new URLSearchParams({ session, org_id: orgId })
  return `${proto}//${window.location.host}/shell/attach?${params.toString()}`
}

// TaskShell renders an xterm.js terminal bridged to the task's sandbox over the
// shell attach WebSocket. It is the browser equivalent of `xagent shell`: it
// frames keystrokes/resizes with the shellwire codec and renders shell output.
export function TaskShell({ session, orgId, onReconnect, reconnecting }: TaskShellProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [phase, setPhase] = useState<Phase>('starting')
  const [exitCode, setExitCode] = useState<number | null>(null)

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    setPhase('starting')
    setExitCode(null)

    const term = new Terminal({
      cursorBlink: true,
      convertEol: false,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      fontSize: 13,
      theme: { background: '#0a0a0a' },
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(container)
    fit.fit()

    const encoder = new TextEncoder()
    const ws = new WebSocket(attachURL(session, orgId), [SUBPROTOCOL])
    ws.binaryType = 'arraybuffer'

    // received guards the exit/close bookkeeping: if the socket closes before any
    // frame arrives, the relay rejected a second operator ("already attached"),
    // which we surface differently from a mid-session drop.
    let received = false
    // gotExit records a clean shell exit so the socket's close handler doesn't
    // overwrite the exit overlay with a generic disconnect message.
    let gotExit = false

    // sendResize frames the terminal's current dimensions — the browser
    // equivalent of the CLI's SIGWINCH handling.
    const sendResize = () => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(encodeResize(term.rows, term.cols))
      }
    }

    const onData = term.onData((d) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(encodeData(encoder.encode(d)))
      }
    })

    ws.onmessage = (ev) => {
      const frame = parse(new Uint8Array(ev.data as ArrayBuffer))
      if (!received) {
        // First byte from the sandbox (data or keepalive) means the driver leg is
        // up and the shell is live.
        received = true
        setPhase('connected')
        sendResize()
      }
      switch (frame.type) {
        case FrameType.Data:
          if (frame.data) term.write(frame.data)
          break
        case FrameType.Exit:
          gotExit = true
          setExitCode(frame.code ?? 0)
          setPhase('exited')
          term.write(`\r\n\x1b[90m[process exited: ${frame.code ?? 0}]\x1b[0m\r\n`)
          ws.close()
          break
        case FrameType.Ping:
          // Keepalive; ignore. It still counts as first contact above.
          break
      }
    }

    ws.onerror = () => {
      if (!gotExit) setPhase('error')
    }

    ws.onclose = () => {
      if (gotExit) return
      // A close with no bytes ever received is the relay rejecting a second
      // operator; a close after the session was live is an unexpected drop.
      setPhase(received ? 'error' : 'detached')
    }

    // Follow container size changes (splitter drags, window resizes) the way the
    // CLI follows SIGWINCH: refit and send the new dimensions.
    const observer = new ResizeObserver(() => {
      fit.fit()
      sendResize()
    })
    observer.observe(container)

    term.focus()

    return () => {
      observer.disconnect()
      onData.dispose()
      // Drop the message handlers before closing so a teardown-triggered close
      // doesn't flip the phase on a stale terminal.
      ws.onmessage = null
      ws.onerror = null
      ws.onclose = null
      ws.close()
      term.dispose()
    }
  }, [session, orgId])

  const overlay = renderOverlay(phase, exitCode, onReconnect, reconnecting)

  return (
    <div className="relative h-full w-full bg-[#0a0a0a]">
      <div ref={containerRef} className="h-full w-full p-2" />
      {overlay}
    </div>
  )
}

function renderOverlay(
  phase: Phase,
  exitCode: number | null,
  onReconnect: () => void,
  reconnecting?: boolean,
) {
  if (phase === 'connected') return null

  if (phase === 'starting') {
    return (
      <Centered>
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        <p className="text-sm text-muted-foreground">Starting sandbox…</p>
      </Centered>
    )
  }

  const message =
    phase === 'exited'
      ? `Shell exited${exitCode !== null ? ` (code ${exitCode})` : ''}.`
      : phase === 'detached'
        ? 'This shell is already attached in another session.'
        : 'The shell connection was lost.'

  return (
    <Centered>
      <p className="text-sm text-muted-foreground">{message}</p>
      <Button size="sm" variant="outline" onClick={onReconnect} disabled={reconnecting}>
        {reconnecting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
        Reconnect
      </Button>
    </Centered>
  )
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-[#0a0a0a]/80">
      {children}
    </div>
  )
}
