import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery } from '@connectrpc/connect-query'
import { listEvents } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Event } from '@/gen/xagent/v1/xagent_pb'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { RelativeTime } from '@/components/ui/relative-time'
import { Button } from '@/components/ui/button'
import { TruncatedText } from '@/components/ui/truncated-text'
import { Plus } from 'lucide-react'

export const Route = createFileRoute('/events/')({
  component: EventsPage,
})

function EventsPage() {
  const { data, isLoading, error } = useQuery(listEvents, {}, {
    refetchInterval: 3000,
  })

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Loading events...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-destructive">Error: {error.message}</div>
      </div>
    )
  }

  const events = data?.events ?? []

  return (
    <div className="container mx-auto py-8 px-4">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Events</h1>
        <Link to="/events/new">
          <Button>
            <Plus className="h-4 w-4" />
            Event
          </Button>
        </Link>
      </div>
      {events.length === 0 ? (
        <div className="text-muted-foreground text-center py-8">
          No events found
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Data</TableHead>
              <TableHead>Created</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {events.map((event) => (
              <EventRow key={String(event.id)} event={event} />
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

function EventRow({ event }: { event: Event }) {
  const dataContent = event.data || '-'

  return (
    <TableRow>
      <TableCell>{String(event.id)}</TableCell>
      <TableCell>
        <Link
          to="/events/$id"
          params={{ id: String(event.id) }}
          className="text-primary hover:underline"
        >
          {event.description || '-'}
        </Link>
      </TableCell>
      <TableCell className="max-w-xs">
        {event.url ? (
          <TruncatedText
            text={dataContent}
            as="a"
            asProps={{
              href: event.url,
              target: '_blank',
              rel: 'noopener noreferrer',
              className: 'text-primary hover:underline',
            }}
          />
        ) : (
          <TruncatedText text={dataContent} />
        )}
      </TableCell>
      <TableCell className="text-muted-foreground">
        {event.createdAt ? <RelativeTime date={timestampDate(event.createdAt)} /> : '-'}
      </TableCell>
    </TableRow>
  )
}

