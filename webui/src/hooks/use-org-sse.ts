import { useEffect } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { createConnectQueryKey } from '@connectrpc/connect-query'
import {
  getTaskDetails,
  listTasks,
  listExternalEvents,
  getEvent,
  listWorkspaces,
  listOrgMembers,
  listKeys,
  listEventsByTask,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { useNotificationSSE, useTimelineFollowers } from '@/lib/services'
import type { TimelineFollowers } from '@/lib/timeline-follow'
import type { Notification, NotificationResource } from '@/lib/notification-sse'

function invalidateResource(
  qc: QueryClient,
  timelineFollowers: TimelineFollowers,
  r: NotificationResource,
) {
  console.debug('[sse] invalidate', r)
  switch (r.type) {
    case 'task':
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: getTaskDetails,
          input: { id: BigInt(r.id) },
          cardinality: 'finite',
        }),
      })
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({ schema: listTasks, cardinality: 'finite' }),
      })
      break
    case 'task_logs':
      // The logs table is gone; reports and lifecycle transitions are events on
      // the task's stream. A task_logs change means the stream grew. The
      // timeline is an append-only bidirectional infinite query, so instead of
      // invalidating (which would re-fetch every loaded page), drive its
      // live-follow: fetch only the newer page and append it at the tail.
      timelineFollowers.notify(String(r.id))
      break
    case 'task_links':
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: getTaskDetails,
          input: { id: BigInt(r.id) },
          cardinality: 'finite',
        }),
      })
      break
    case 'event':
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({ schema: listExternalEvents, cardinality: 'finite' }),
      })
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: getEvent,
          input: { id: BigInt(r.id) },
          cardinality: 'finite',
        }),
      })
      break
    case 'workspaces':
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({ schema: listWorkspaces, cardinality: 'finite' }),
      })
      break
    case 'org_members':
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({ schema: listOrgMembers, cardinality: 'finite' }),
      })
      break
    case 'keys':
      qc.invalidateQueries({
        queryKey: createConnectQueryKey({ schema: listKeys, cardinality: 'finite' }),
      })
      break
    default:
      console.warn('[sse] unhandled resource type', r)
  }
}

// The task timeline is a bidirectional infinite query over immutable
// append-only pages (see use-task-timeline). Its connect-query key carries the
// input (task id) and transport, but every mounted timeline shares the same
// service/method/cardinality, so matching on those three exempts them all.
const timelineProps = (
  createConnectQueryKey({ schema: listEventsByTask, cardinality: 'infinite' }) as [
    string,
    { serviceName: string; methodName?: string; cardinality?: string },
  ]
)[1]

function isTimelineQueryKey(queryKey: unknown): boolean {
  if (!Array.isArray(queryKey) || queryKey[0] !== 'connect-query') return false
  const props = queryKey[1] as
    { serviceName?: string; methodName?: string; cardinality?: string } | undefined
  return (
    props?.serviceName === timelineProps.serviceName &&
    props?.methodName === timelineProps.methodName &&
    props?.cardinality === timelineProps.cardinality
  )
}

// handleReconnect resyncs after the notification SSE drops and comes back: while
// disconnected we miss change notifications, so everything cached may be stale.
// It invalidates every query EXCEPT the append-only task timelines — refetching
// an invalidated infinite query re-fetches every loaded page, and since
// reconnects happen routinely (network blips, tab background→foreground) that
// would refetch the whole loaded timeline on each one. Instead the timelines
// catch up via a single tail-follow driven by notifyAll().
export function handleReconnect(qc: QueryClient, timelineFollowers: TimelineFollowers) {
  qc.invalidateQueries({ predicate: (query) => !isTimelineQueryKey(query.queryKey) })
  timelineFollowers.notifyAll()
}

function handleNotification(
  qc: QueryClient,
  timelineFollowers: TimelineFollowers,
  n: Notification,
) {
  console.debug('[sse] notification', n)
  if (n.type === 'ready') {
    return
  }
  for (const r of n.resources ?? []) {
    invalidateResource(qc, timelineFollowers, r)
  }
}

export function useOrgSSE() {
  const queryClient = useQueryClient()
  const sse = useNotificationSSE()
  const timelineFollowers = useTimelineFollowers()

  useEffect(() => {
    const removeNotification = sse.addNotificationListener((n) => {
      handleNotification(queryClient, timelineFollowers, n)
    })
    const removeReconnect = sse.addReconnectListener(() => {
      handleReconnect(queryClient, timelineFollowers)
    })
    return () => {
      removeNotification()
      removeReconnect()
    }
  }, [queryClient, sse, timelineFollowers])
}
