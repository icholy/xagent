// startVisibilityPoll invokes `tick` every intervalMs, but only while the tab is
// foregrounded. It backs the timeline live-follow: SSE task_logs signals
// normally drive the tail-follow, but a dropped or missed signal would stall new
// entries until the next signal or an SSE reconnect, so this interval is a
// safety net. A backgrounded tab closes the SSE (foregrounding reconnects and
// resyncs), so polling while hidden is wasted work — the interval is stopped on
// hide and restarted on show. Returns a cleanup that stops the interval and
// removes the visibility listener.
export function startVisibilityPoll(tick: () => void, intervalMs: number): () => void {
  let id: ReturnType<typeof setInterval> | undefined

  const start = () => {
    if (id === undefined) id = setInterval(tick, intervalMs)
  }
  const stop = () => {
    if (id !== undefined) {
      clearInterval(id)
      id = undefined
    }
  }
  const onVisibilityChange = () => {
    if (document.hidden) stop()
    else start()
  }

  if (!document.hidden) start()
  document.addEventListener('visibilitychange', onVisibilityChange)

  return () => {
    stop()
    document.removeEventListener('visibilitychange', onVisibilityChange)
  }
}
