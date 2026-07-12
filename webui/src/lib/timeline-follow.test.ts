import { describe, it, expect, vi } from 'vitest'
import { TimelineFollowers } from './timeline-follow'

describe('TimelineFollowers', () => {
  it('notify fires only the follows registered for the given task', () => {
    const f = new TimelineFollowers()
    const a = vi.fn()
    const b = vi.fn()
    f.register('1', a)
    f.register('2', b)

    f.notify('1')

    expect(a).toHaveBeenCalledTimes(1)
    expect(b).not.toHaveBeenCalled()
  })

  it('notify is a no-op for a task with no mounted timeline', () => {
    const f = new TimelineFollowers()
    expect(() => f.notify('missing')).not.toThrow()
  })

  it('notifyAll fires every registered follow across all tasks exactly once', () => {
    const f = new TimelineFollowers()
    const a = vi.fn()
    const b = vi.fn()
    const c = vi.fn()
    f.register('1', a)
    f.register('2', b)
    // A second mount of the same task (StrictMode / route transition).
    f.register('2', c)

    f.notifyAll()

    expect(a).toHaveBeenCalledTimes(1)
    expect(b).toHaveBeenCalledTimes(1)
    expect(c).toHaveBeenCalledTimes(1)
  })

  it('notifyAll is a no-op when no timeline is mounted', () => {
    const f = new TimelineFollowers()
    expect(() => f.notifyAll()).not.toThrow()
  })

  it('notifyAll skips a follow after it unregisters', () => {
    const f = new TimelineFollowers()
    const a = vi.fn()
    const unregister = f.register('1', a)
    unregister()

    f.notifyAll()

    expect(a).not.toHaveBeenCalled()
  })
})
