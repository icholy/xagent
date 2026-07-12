// A task's timeline is a bidirectional infinite query owned by the mounted
// tasks.$id route (see use-task-timeline). Live `task_logs` signals arrive on
// the org-wide SSE stream (use-org-sse), which can't reach that query's
// fetchNextPage directly. A mounted timeline registers a follow callback here
// keyed by task id; the SSE handler looks it up and fires it, so a signal
// fetches only the newer page (an append) instead of invalidating and
// re-fetching the whole stream.

type FollowFn = () => void

const followers = new Map<string, Set<FollowFn>>()

// registerTimelineFollower adds follow for taskId and returns an unregister
// function. A Set allows more than one mount of the same task (e.g. during a
// route transition) without either clobbering the other.
export function registerTimelineFollower(taskId: string, follow: FollowFn): () => void {
  let set = followers.get(taskId)
  if (!set) {
    set = new Set()
    followers.set(taskId, set)
  }
  set.add(follow)
  return () => {
    set.delete(follow)
    if (set.size === 0) followers.delete(taskId)
  }
}

// notifyTimelineFollowers fires every follow registered for taskId. It is a
// no-op when the task's timeline isn't mounted — there is nothing cached to
// update, and the timeline opens at the tail when next viewed.
export function notifyTimelineFollowers(taskId: string): void {
  const set = followers.get(taskId)
  if (!set) return
  for (const follow of set) follow()
}
