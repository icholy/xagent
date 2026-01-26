import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery } from '@connectrpc/connect-query'
import { getEvent, listEventTasks } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { RelativeTime } from '@/components/relative-time'

export const Route = createFileRoute('/events/$id')({
  component: EventDetail,
})

function EventDetail() {
  const { id } = Route.useParams()
  const eventId = BigInt(id)

  const { data: eventData, isLoading: eventLoading, error: eventError } = useQuery(
    getEvent,
    { id: eventId },
    { refetchInterval: 6000 }
  )

  const { data: tasksData, isLoading: tasksLoading } = useQuery(
    listEventTasks,
    { eventId },
    { refetchInterval: 6000 }
  )

  if (eventLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Loading event...</div>
      </div>
    )
  }

  if (eventError) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-destructive">Error: {eventError.message}</div>
      </div>
    )
  }

  const event = eventData?.event

  if (!event) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Event not found</div>
      </div>
    )
  }

  const taskIds = tasksData?.taskIds ?? []

  return (
    <div className="container mx-auto py-8 px-4">
      <h1 className="text-2xl font-bold mb-6">Event {String(event.id)}</h1>

      <div className="space-y-6">
        <div className="rounded-lg border p-6 space-y-4">
          <div>
            <h2 className="text-sm font-medium text-muted-foreground">Description</h2>
            <p className="mt-1">{event.description || '-'}</p>
          </div>

          <div>
            <h2 className="text-sm font-medium text-muted-foreground">URL</h2>
            <p className="mt-1">
              {event.url ? (
                <a
                  href={event.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-primary hover:underline break-all"
                >
                  {event.url}
                </a>
              ) : (
                '-'
              )}
            </p>
          </div>

          <div>
            <h2 className="text-sm font-medium text-muted-foreground">Created</h2>
            <p className="mt-1">
              {event.createdAt ? <RelativeTime date={timestampDate(event.createdAt)} /> : '-'}
            </p>
          </div>

          {event.data && (
            <div>
              <h2 className="text-sm font-medium text-muted-foreground">Data</h2>
              <pre className="mt-1 p-4 bg-muted rounded-md overflow-x-auto text-sm">
                {formatJson(event.data)}
              </pre>
            </div>
          )}
        </div>

        <div className="rounded-lg border p-6">
          <h2 className="text-lg font-semibold mb-4">Associated Tasks</h2>
          {tasksLoading ? (
            <p className="text-muted-foreground">Loading tasks...</p>
          ) : taskIds.length === 0 ? (
            <p className="text-muted-foreground">No associated tasks</p>
          ) : (
            <ul className="space-y-2">
              {taskIds.map((taskId) => (
                <li key={String(taskId)}>
                  <Link
                    to="/tasks/$id"
                    params={{ id: String(taskId) }}
                    className="text-primary hover:underline"
                  >
                    Task {String(taskId)}
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  )
}

function formatJson(data: string): string {
  try {
    return JSON.stringify(JSON.parse(data), null, 2)
  } catch {
    return data
  }
}
