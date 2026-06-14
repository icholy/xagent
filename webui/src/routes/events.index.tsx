import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import {
  getRoutingRules,
  listExternalEvents,
  setRoutingRules,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Event, RoutingRule } from '@/gen/xagent/v1/xagent_pb'
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
  const [limit, setLimit] = useState(25)
  const { data, isLoading, error } = useQuery(
    listExternalEvents,
    { limit },
    { refetchInterval: 60000 },
  )

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
  // Only external events carry description/url/data; other arms render as '-'.
  const external = event.payload.case === 'external' ? event.payload.value : undefined
  const dataContent = external?.data || '-'
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
          {external?.description || '-'}
        </Link>
      </TableCell>
      <TableCell className="max-w-xs truncate">
        {external?.url ? (
          <a
            href={external.url}
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

// Compact labels for a rule's matchers, shown as badges in the routing rules
// table. The url prefix value is omitted because URLs are too long to fit on a
// single line.
function ruleMatchBadges(rule: RoutingRule): string[] {
  const labels: string[] = []
  if (rule.urlPrefix) labels.push('urlprefix')
  if (rule.prefix) labels.push(`prefix:${rule.prefix}`)
  if (rule.mention) labels.push(`mention:${rule.mention}`)
  if (rule.assignee) labels.push(`assignee:${rule.assignee}`)
  if (rule.value) labels.push(`value:${rule.value}`)
  return labels
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
        <CardDescription>
          Configure how events get routed to tasks and workspaces. Rules are evaluated top to
          bottom; the first match wins.
        </CardDescription>
        <CardAction>
          <Link to="/routing/new" search={{ org: orgId }}>
            <Button>
              <Plus className="h-4 w-4" />
              Rule
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
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Event Type</TableHead>
                <TableHead className="hidden md:table-cell">Filters</TableHead>
                <TableHead className="hidden md:table-cell">Action</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rules.map((rule, index) => (
                <TableRow key={index}>
                  <TableCell className="font-medium whitespace-nowrap">
                    {eventTypeLabel(rule.source, rule.type)}
                  </TableCell>
                  <TableCell className="hidden md:table-cell">
                    <div className="flex flex-wrap gap-1">
                      {ruleMatchBadges(rule).map((label) => (
                        <Badge
                          key={label}
                          variant="outline"
                          className="font-mono max-w-full truncate"
                        >
                          {label}
                        </Badge>
                      ))}
                    </div>
                  </TableCell>
                  <TableCell className="hidden md:table-cell">
                    <div className="flex flex-wrap gap-1">
                      {rule.wakeup && <Badge variant="secondary">wake</Badge>}
                      {rule.create && <Badge variant="secondary">create</Badge>}
                      {!rule.wakeup && !rule.create && (
                        <span className="text-muted-foreground whitespace-nowrap">None</span>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
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
        )}
      </CardContent>
    </Card>
  )
}
