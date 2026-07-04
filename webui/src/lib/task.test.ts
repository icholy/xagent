import { describe, it, expect } from 'vitest'
import { toTaskTab } from './task'

describe('toTaskTab', () => {
  it('passes through the non-default panels', () => {
    expect(toTaskTab('shell')).toBe('shell')
    expect(toTaskTab('links')).toBe('links')
  })

  it('falls back to timeline for the default and any unknown value', () => {
    expect(toTaskTab('timeline')).toBe('timeline')
    expect(toTaskTab('bogus')).toBe('timeline')
    expect(toTaskTab(undefined)).toBe('timeline')
    expect(toTaskTab(42)).toBe('timeline')
  })
})
