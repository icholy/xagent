import { describe, it, expect } from 'vitest'
import type { Duration } from '@bufbuild/protobuf/wkt'
import { autoArchiveSelectValue, durationFromAutoArchiveSelect, formatCountdown } from './duration'

function duration(seconds: number, nanos = 0): Duration {
  return { seconds: BigInt(seconds), nanos, $typeName: 'google.protobuf.Duration' }
}

describe('formatCountdown', () => {
  it('renders the two largest units for a multi-day remaining time', () => {
    const ms = (2 * 24 * 60 * 60 + 3 * 60 * 60 + 30 * 60) * 1000
    expect(formatCountdown(ms)).toBe('2d 3h')
  })
})

describe('autoArchiveSelectValue', () => {
  it('maps unset and zero durations to "never"', () => {
    expect(autoArchiveSelectValue(undefined)).toBe('never')
    expect(autoArchiveSelectValue(duration(0))).toBe('never')
  })

  it('maps negative durations to "immediate"', () => {
    expect(autoArchiveSelectValue(duration(-1))).toBe('immediate')
    expect(autoArchiveSelectValue(duration(0, -1))).toBe('immediate')
  })

  it('maps positive durations to their whole-second count', () => {
    expect(autoArchiveSelectValue(duration(24 * 3600))).toBe('86400')
    expect(autoArchiveSelectValue(duration(168 * 3600))).toBe('604800')
  })

  it('preserves off-preset durations losslessly instead of rounding to a preset', () => {
    // 30 minutes must not collapse onto the "1 hour" preset value.
    expect(autoArchiveSelectValue(duration(30 * 60))).toBe('1800')
    expect(autoArchiveSelectValue(duration(3 * 3600))).toBe('10800')
  })
})

describe('durationFromAutoArchiveSelect', () => {
  it('returns an explicit zero duration for "never" so UpdateTask persists it', () => {
    // UpdateTask treats an omitted auto_archive as "leave alone", so "never" must
    // round-trip as a concrete zero duration rather than undefined.
    expect(durationFromAutoArchiveSelect('never')).toEqual(duration(0))
  })

  it('returns a negative duration for "immediate"', () => {
    const d = durationFromAutoArchiveSelect('immediate')
    expect(d.seconds).toBeLessThan(0n)
  })

  it('parses a seconds-encoded selection back into a duration', () => {
    expect(durationFromAutoArchiveSelect('86400')).toEqual(duration(24 * 3600))
    expect(durationFromAutoArchiveSelect('1800')).toEqual(duration(30 * 60))
  })

  it('round-trips through autoArchiveSelectValue', () => {
    for (const value of ['never', 'immediate', '3600', '86400', '604800', '1800']) {
      expect(autoArchiveSelectValue(durationFromAutoArchiveSelect(value))).toBe(value)
    }
  })
})
