import { describe, it, expect, vi } from 'vitest'
import { QueryClient } from '@tanstack/react-query'
import { createConnectQueryKey } from '@connectrpc/connect-query'
import {
  listTasks,
  getTaskDetails,
  listEventsByTask,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Transport } from '@connectrpc/connect'
import { handleReconnect } from './use-org-sse'
import { TimelineFollowers } from '@/lib/timeline-follow'

// A bare object stands in for the transport: connect-query only derives a
// stable string key from its reference (a WeakMap lookup), never calls it.
const transport = {} as Transport

// The timeline key is built exactly as use-task-timeline builds it, so the
// exemption is verified against the real infinite key, not a hand-rolled shape.
function timelineKey(taskId: bigint) {
  return createConnectQueryKey({
    schema: listEventsByTask,
    input: { taskId, pageSize: 50, pageToken: '' },
    transport,
    cardinality: 'infinite',
    pageParamKey: 'pageToken',
  })
}

describe('handleReconnect', () => {
  it('invalidates non-timeline queries but leaves loaded timeline pages untouched', () => {
    const qc = new QueryClient()
    const followers = new TimelineFollowers()

    const tasksKey = createConnectQueryKey({ schema: listTasks, cardinality: 'finite' })
    const detailsKey = createConnectQueryKey({
      schema: getTaskDetails,
      input: { id: 1n },
      cardinality: 'finite',
    })
    const tlKey = timelineKey(1n)

    // The cached values' shapes are irrelevant here — the tests assert only on
    // invalidation state — so bare stubs stand in for the tagged message types.
    qc.setQueryData(tasksKey, { tasks: [] } as never)
    qc.setQueryData(detailsKey, { task: {} } as never)
    // A timeline with two loaded pages: reconnect must NOT mark these stale, or
    // React Query would refetch both.
    qc.setQueryData(tlKey, {
      pages: [{ events: [] }, { events: [] }],
      pageParams: ['', 'p2'],
    } as never)

    handleReconnect(qc, followers)

    expect(qc.getQueryState(tasksKey)?.isInvalidated).toBe(true)
    expect(qc.getQueryState(detailsKey)?.isInvalidated).toBe(true)
    expect(qc.getQueryState(tlKey)?.isInvalidated).toBe(false)
  })

  it('drives exactly one follow per mounted timeline instead of refetching pages', () => {
    const qc = new QueryClient()
    const followers = new TimelineFollowers()
    const followA = vi.fn()
    const followB = vi.fn()
    followers.register('1', followA)
    followers.register('2', followB)

    handleReconnect(qc, followers)

    expect(followA).toHaveBeenCalledTimes(1)
    expect(followB).toHaveBeenCalledTimes(1)
  })

  it('exempts every mounted timeline regardless of task id', () => {
    const qc = new QueryClient()
    const followers = new TimelineFollowers()

    const a = timelineKey(1n)
    const b = timelineKey(2n)
    qc.setQueryData(a, { pages: [{ events: [] }], pageParams: [''] } as never)
    qc.setQueryData(b, { pages: [{ events: [] }], pageParams: [''] } as never)

    handleReconnect(qc, followers)

    expect(qc.getQueryState(a)?.isInvalidated).toBe(false)
    expect(qc.getQueryState(b)?.isInvalidated).toBe(false)
  })
})
