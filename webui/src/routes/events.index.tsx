import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import {
  getRoutingRules,
  listEvents,
  setRoutingRules,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Event } from '@/gen/xagent/v1/xagent_pb'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { RelativeTime } from '@/components/relative-time'
import { eventTypeLabel } from '@/lib/routing-rules'
import { ChevronDown, ChevronUp, Loader2, Pencil, Plus, Trash2 } from 'lucide-react'
import { useOrgId } from '@/hooks/use-org-id'

export const Route = createFileRoute('/events/')({
  staticData: { orgSwitchRedirect: '/events' },
  component: EventsPage,
})

function EventsPage() {
  return (
    <div className="container mx-auto py-8 px-4">
      <h1 className="text-2xl font-bold mb-6">Events</h1>
      <div className="space-y-6">
        <RoutingRulesCard />
        <RecentEventsCard />
      </div>
    </div>
  )
}

function RecentEventsCard() {
  const orgId = useOrgId()
  const [limit, setLimit] = useState(25)
  const { data, isLoading, error } = useQuery(listEvents, { limit }, { refetchInterval: 60000 })

  const events = data?.events ?? []

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <CardTitle>Recent Events</CardTitle>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-2">
              <span className="text-sm text-muted-foreground">Show</span>
              <Select value={String(limit)} onValueChange={(value) => setLimit(Number(value))}>
                <SelectTrigger className="w-20">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="25">25</SelectItem>
                  <SelectItem value="50">50</SelectItem>
                  <SelectItem value="75">75</SelectItem>
                  <SelectItem value="100">100</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Link to="/events/new" search={{ org: orgId }}>
              <Button>
                <Plus className="h-4 w-4" />
                Event
              </Button>
            </Link>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="text-muted-foreground">Loading...</div>
        ) : error ? (
          <div className="text-destructive">Error: {error.message}</div>
        ) : events.length === 0 ? (
          <div className="text-muted-foreground text-center py-8">No events found</div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="hidden md:table-cell">ID</TableHead>
                <TableHead>Description</TableHead>
                <TableHead>Data</TableHead>
                <TableHead className="hidden md:table-cell">Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {events.map((event) => (
                <EventRow key={String(event.id)} event={event} />
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function EventRow({ event }: { event: Event }) {
  const orgId = useOrgId()
  const dataContent = event.data || '-'
  const truncatedData = dataContent.length > 100 ? dataContent.slice(0, 100) + '...' : dataContent

  return (
    <TableRow>
      <TableCell className="hidden md:table-cell">{String(event.id)}</TableCell>
      <TableCell>
        <Link
          to="/events/$id"
          search={{ org: orgId }}
          params={{ id: String(event.id) }}
          className="text-primary hover:underline"
        >
          {event.description || '-'}
        </Link>
      </TableCell>
      <TableCell className="max-w-xs truncate">
        {event.url ? (
          <a
            href={event.url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary hover:underline"
          >
            {truncatedData}
          </a>
        ) : (
          truncatedData
        )}
      </TableCell>
      <TableCell className="hidden md:table-cell text-muted-foreground">
        {event.createdAt ? <RelativeTime date={timestampDate(event.createdAt)} /> : '-'}
      </TableCell>
    </TableRow>
  )
}

function RoutingRulesCard() {
  const orgId = useOrgId()
  const { data, isLoading, refetch } = useQuery(
    getRoutingRules,
    {},
    {
      refetchInterval: 6000,
    },
  )
  const updateMutation = useMutation(setRoutingRules, {
    onSuccess: () => refetch(),
  })

  const rules = data?.rules ?? []

  const handleDelete = async (index: number) => {
    const updated = rules.filter((_, i) => i !== index)
    await updateMutation.mutateAsync({ rules: updated })
  }

  const handleMove = async (index: number, direction: -1 | 1) => {
    const target = index + direction
    if (target < 0 || target >= rules.length) return
    const updated = [...rules]
    ;[updated[index], updated[target]] = [updated[target], updated[index]]
    await updateMutation.mutateAsync({ rules: updated })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Routing Rules</CardTitle>
        <CardDescription>Configure how events get routed to tasks and workspaces.</CardDescription>
        <CardAction>
          <Link to="/routing/new" search={{ org: orgId }}>
            <Button>
              <Plus className="h-4 w-4" />
              Add Rule
            </Button>
          </Link>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-4">
        {updateMutation.error && (
          <div className="text-destructive text-sm">{updateMutation.error.message}</div>
        )}
        {isLoading ? (
          <div className="text-muted-foreground">Loading...</div>
        ) : rules.length === 0 ? (
          <div className="text-muted-foreground text-center py-8">No routing rules configured</div>
        ) : (
          <>
            <p className="text-sm text-muted-foreground">
              Rules are evaluated top to bottom; the first match wins.
            </p>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Event Type</TableHead>
                  <TableHead>Prefix</TableHead>
                  <TableHead>Mention</TableHead>
                  <TableHead>Assignee</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.map((rule, index) => (
                  <TableRow key={index}>
                    <TableCell className="font-medium">
                      {eventTypeLabel(rule.source, rule.type)}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{rule.prefix || '-'}</TableCell>
                    <TableCell className="text-muted-foreground">{rule.mention || '-'}</TableCell>
                    <TableCell className="text-muted-foreground">{rule.assignee || '-'}</TableCell>
                    <TableCell>
                      {rule.create ? (
                        <Badge variant="secondary">Creates task</Badge>
                      ) : (
                        <span className="text-muted-foreground">Wake only</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => handleMove(index, -1)}
                          disabled={updateMutation.isPending || index === 0}
                          aria-label="Move rule up"
                        >
                          <ChevronUp className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => handleMove(index, 1)}
                          disabled={updateMutation.isPending || index === rules.length - 1}
                          aria-label="Move rule down"
                        >
                          <ChevronDown className="h-4 w-4" />
                        </Button>
                        <Link
                          to="/routing/$index"
                          params={{ index: String(index) }}
                          search={{ org: orgId }}
                        >
                          <Button variant="outline" size="sm" disabled={updateMutation.isPending}>
                            <Pencil className="h-4 w-4" />
                          </Button>
                        </Link>
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => handleDelete(index)}
                          disabled={updateMutation.isPending}
                        >
                          {updateMutation.isPending ? (
                            <Loader2 className="h-4 w-4 animate-spin" />
                          ) : (
                            <Trash2 className="h-4 w-4" />
                          )}
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </>
        )}
      </CardContent>
    </Card>
  )
}
