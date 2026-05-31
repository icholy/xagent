import type { Duration } from '@bufbuild/protobuf/wkt'

// durationFromHours parses an integer string number of hours into a protobuf
// Duration, or returns undefined for empty / "never". Used by the auto-archive
// controls (Create Task screen, routing-rule editor) so created tasks archive
// the same way.
export function durationFromHours(value: string): Duration | undefined {
  if (!value || value === 'never') return undefined
  const hours = Number.parseInt(value, 10)
  if (!Number.isFinite(hours) || hours <= 0) return undefined
  return { seconds: BigInt(hours * 3600), nanos: 0, $typeName: 'google.protobuf.Duration' }
}

// hoursFromDuration is the inverse of durationFromHours: it renders a stored
// Duration back to the whole-hours string the archive-after Select uses, or ''
// (never) when unset or non-positive.
export function hoursFromDuration(d: Duration | undefined): string {
  if (!d) return ''
  const hours = Number(d.seconds) / 3600
  if (!Number.isFinite(hours) || hours <= 0) return ''
  return String(Math.round(hours))
}
