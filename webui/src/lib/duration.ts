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

// durationToMillis converts a protobuf Duration to milliseconds.
export function durationToMillis(d: Duration): number {
  return Number(d.seconds) * 1000 + d.nanos / 1_000_000
}

// formatCountdown renders a coarse, human-readable remaining time using the one
// or two largest units, e.g. "45s", "5m", "2h 10m", "3d 4h". Negative inputs
// clamp to "0s".
export function formatCountdown(ms: number): string {
  const totalSeconds = Math.max(0, Math.round(ms / 1000))
  if (totalSeconds < 60) return `${totalSeconds}s`
  const totalMinutes = Math.floor(totalSeconds / 60)
  if (totalMinutes < 60) return `${totalMinutes}m`
  const totalHours = Math.floor(totalMinutes / 60)
  if (totalHours < 24) {
    const minutes = totalMinutes % 60
    return minutes ? `${totalHours}h ${minutes}m` : `${totalHours}h`
  }
  const totalDays = Math.floor(totalHours / 24)
  const hours = totalHours % 24
  return hours ? `${totalDays}d ${hours}h` : `${totalDays}d`
}
