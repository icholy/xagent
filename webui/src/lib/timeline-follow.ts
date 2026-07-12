// TimelineFollowers bridges the org-wide SSE stream to the task timelines
// mounted in the React tree. A task's timeline (see use-task-timeline) is a
// bidirectional infinite query whose fetchNextPage lives inside a component; the
// org-wide SSE stream (use-org-sse) — where live task_logs signals arrive —
// can't reach it directly. A mounted timeline registers a follow callback here
// keyed by task id, and the SSE handler calls notify() so a signal fetches only
// the newer page (an append) instead of invalidating and re-fetching the whole
// stream.
//
// It is created once at the app root and injected via the Services context,
// alongside AuthTransport, NotificationSSE, and ShellSessions.

type FollowFn = () => void

export class TimelineFollowers {
  private readonly followers = new Map<string, Set<FollowFn>>()

  // register adds follow for taskId and returns an unregister function. A Set
  // allows more than one mount of the same task (a StrictMode double-mount, or a
  // route transition) without either clobbering the other.
  register(taskId: string, follow: FollowFn): () => void {
    let set = this.followers.get(taskId)
    if (!set) {
      set = new Set()
      this.followers.set(taskId, set)
    }
    set.add(follow)
    return () => {
      const s = this.followers.get(taskId)
      if (!s) return
      s.delete(follow)
      if (s.size === 0) this.followers.delete(taskId)
    }
  }

  // notify fires every follow registered for taskId. It is a no-op when the
  // task's timeline isn't mounted — there is nothing cached to update, and the
  // timeline opens at the tail when next viewed.
  notify(taskId: string): void {
    const set = this.followers.get(taskId)
    if (!set) return
    for (const follow of set) follow()
  }

  // notifyAll fires every registered follow across all task ids. It's the
  // reconnect counterpart to notify: after the SSE drops and comes back, each
  // mounted timeline catches up on anything missed during the gap with a single
  // tail-follow (fetch newer-than-tail, append) rather than refetching every
  // loaded page. A no-op when no timeline is mounted.
  notifyAll(): void {
    for (const set of this.followers.values()) {
      for (const follow of set) follow()
    }
  }
}
