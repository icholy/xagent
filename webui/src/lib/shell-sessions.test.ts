import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { ShellSessions } from './shell-sessions'
import type { Client } from '@connectrpc/connect'
import type { XAgentService } from '@/gen/xagent/v1/xagent_pb'

// The refcount / grace-teardown logic exercised here never calls open(), so the
// client is never invoked; a bare cast stands in for it.
const stubClient = {} as Client<typeof XAgentService>

const GRACE = 100
const KEY = '7'

function newSessions() {
  return new ShellSessions({ client: stubClient, graceMs: GRACE })
}

describe('ShellSessions refcount / grace teardown', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('creates an entry on attach and defers teardown past the grace window', () => {
    const s = newSessions()
    s.attach(KEY, '0')
    expect(s.has(KEY)).toBe(true)

    s.detach(KEY)
    // Still alive just before the grace window elapses.
    vi.advanceTimersByTime(GRACE - 1)
    expect(s.has(KEY)).toBe(true)

    // Torn down once it fully elapses.
    vi.advanceTimersByTime(1)
    expect(s.has(KEY)).toBe(false)
  })

  it('cancels a pending teardown when a re-attach arrives within the grace window', () => {
    const s = newSessions()

    // This is the StrictMode shape: attach → detach → attach, synchronously.
    s.attach(KEY, '0')
    s.detach(KEY)
    s.attach(KEY, '0')

    // The teardown scheduled by the detach must not fire — the re-attach cancelled it.
    vi.advanceTimersByTime(GRACE * 5)
    expect(s.has(KEY)).toBe(true)
  })

  it('only tears down after the last interest is released (ref-counted)', () => {
    const s = newSessions()
    s.attach(KEY, '0')
    s.attach(KEY, '0')

    // One holder remains after a single detach — no teardown scheduled.
    s.detach(KEY)
    vi.advanceTimersByTime(GRACE * 2)
    expect(s.has(KEY)).toBe(true)

    // Releasing the last holder schedules teardown.
    s.detach(KEY)
    vi.advanceTimersByTime(GRACE)
    expect(s.has(KEY)).toBe(false)
  })

  it('detach on an unknown task is a no-op', () => {
    const s = newSessions()
    expect(() => s.detach('nope')).not.toThrow()
    expect(s.has('nope')).toBe(false)
  })

  it('reports the IDLE snapshot for a task with no entry', () => {
    const s = newSessions()
    expect(s.getSnapshot('missing')).toEqual({
      phase: 'idle',
      exitCode: null,
      error: null,
      started: false,
    })
  })

  it('notifies state subscribers when a grace teardown removes the entry', () => {
    const s = newSessions()
    const listener = vi.fn()
    s.subscribe(KEY, listener)

    s.attach(KEY, '0')
    s.detach(KEY)
    vi.advanceTimersByTime(GRACE)

    expect(s.has(KEY)).toBe(false)
    expect(listener).toHaveBeenCalled()
    expect(s.getSnapshot(KEY).phase).toBe('idle')
  })
})
