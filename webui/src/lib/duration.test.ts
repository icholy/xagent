import { describe, it, expect } from 'vitest'
import { formatCountdown } from './duration'

describe('formatCountdown', () => {
  it('renders the two largest units for a multi-day remaining time', () => {
    const ms = (2 * 24 * 60 * 60 + 3 * 60 * 60 + 30 * 60) * 1000
    expect(formatCountdown(ms)).toBe('2d 3h')
  })
})
