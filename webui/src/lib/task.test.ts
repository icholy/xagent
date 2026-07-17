import { describe, it, expect } from 'vitest'
import { toTaskTab } from './task'

describe('toTaskTab', () => {
  it('passes through the non-default views', () => {
    expect(toTaskTab('shell')).toBe('shell')
  })

  it('falls back to timeline for the default and any unknown value', () => {
    expect(toTaskTab('timeline')).toBe('timeline')
    // links stopped being a tab when they moved into the sidebar; stale deep
    // links fall back to the timeline.
    expect(toTaskTab('links')).toBe('timeline')
    expect(toTaskTab('bogus')).toBe('timeline')
    expect(toTaskTab(undefined)).toBe('timeline')
    expect(toTaskTab(42)).toBe('timeline')
  })
})
