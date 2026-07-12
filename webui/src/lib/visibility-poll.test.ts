// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { startVisibilityPoll } from './visibility-poll'

// Drive document.hidden from a variable and fire a real visibilitychange event
// so the poll's own listener runs, mirroring the browser.
let hidden = false
function setHidden(value: boolean) {
  hidden = value
  document.dispatchEvent(new Event('visibilitychange'))
}

beforeEach(() => {
  hidden = false
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('startVisibilityPoll', () => {
  it('drives the tick on each interval while the tab is visible', () => {
    const tick = vi.fn()
    const stop = startVisibilityPoll(tick, 30_000)

    vi.advanceTimersByTime(90_000)

    expect(tick).toHaveBeenCalledTimes(3)
    stop()
  })

  it('does not tick while the document is hidden', () => {
    hidden = true
    const tick = vi.fn()
    const stop = startVisibilityPoll(tick, 30_000)

    vi.advanceTimersByTime(90_000)

    expect(tick).not.toHaveBeenCalled()
    stop()
  })

  it('stops ticking once the tab is backgrounded', () => {
    const tick = vi.fn()
    const stop = startVisibilityPoll(tick, 30_000)

    vi.advanceTimersByTime(30_000)
    expect(tick).toHaveBeenCalledTimes(1)

    setHidden(true)
    vi.advanceTimersByTime(90_000)
    expect(tick).toHaveBeenCalledTimes(1)
    stop()
  })

  it('resumes ticking when the tab is foregrounded again', () => {
    hidden = true
    const tick = vi.fn()
    const stop = startVisibilityPoll(tick, 30_000)

    vi.advanceTimersByTime(30_000)
    expect(tick).not.toHaveBeenCalled()

    setHidden(false)
    vi.advanceTimersByTime(30_000)
    expect(tick).toHaveBeenCalledTimes(1)
    stop()
  })

  it('stops the interval after cleanup', () => {
    const tick = vi.fn()
    const stop = startVisibilityPoll(tick, 30_000)

    vi.advanceTimersByTime(30_000)
    expect(tick).toHaveBeenCalledTimes(1)

    stop()
    vi.advanceTimersByTime(90_000)
    expect(tick).toHaveBeenCalledTimes(1)
  })
})
