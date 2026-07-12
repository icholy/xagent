import { useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import {
  getEventTypes,
  getRoutingRules,
  testEvent,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { EventTypeDef, RoutingRule, TestEventResponse } from '@/gen/xagent/v1/xagent_pb'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  eventTypeId,
  eventTypeLabel,
  findEventType,
  OP_LABELS,
  type ConditionOp,
} from '@/lib/routing-rules'
import { useOrgId } from '@/hooks/use-org-id'
import { Loader2, Plus, Trash2 } from 'lucide-react'

// A free-form details row feeding TestEventRequest.details. Unlike attrs, these
// are source-defined context the router never matches on — it copies them
// verbatim onto the persisted event for the timeline/agent to render.
interface DetailRow {
  key: string
  value: string
}

export function TestEventForm() {
  const orgId = useOrgId()

  const { data: eventTypesData, isLoading: eventTypesLoading } = useQuery(getEventTypes, {})
  const eventTypes = useMemo(() => eventTypesData?.eventTypes ?? [], [eventTypesData])

  // The configured rules, rendered below so a match can be highlighted by its
  // rule_index — closing the compose → see-which-rule-fired loop.
  const { data: rulesData } = useQuery(getRoutingRules, {}, { refetchInterval: 6000 })
  const rules = rulesData?.rules ?? []

  const [source, setSource] = useState('')
  const [type, setType] = useState('')
  // Attr values keyed by AttrDef.key (incl. the derived "body"/"url"). Kept
  // across type switches so common attrs like body/url survive; only attrs the
  // selected type offers are rendered and submitted.
  const [attrValues, setAttrValues] = useState<Record<string, string>>({})
  const [details, setDetails] = useState<DetailRow[]>([])
  const [description, setDescription] = useState('')
  const [confirmFire, setConfirmFire] = useState(false)

  const mutation = useMutation(testEvent)
  const result = mutation.data

  const selectedType = findEventType(eventTypes, source, type)
  const selectedId = selectedType ? eventTypeId(selectedType.source, selectedType.type) : ''
  const availableAttrs = selectedType?.attrs ?? []

  const handleEventTypeChange = (id: string) => {
    const target = eventTypes.find((t) => eventTypeId(t.source, t.type) === id)
    if (!target) return
    setSource(target.source)
    setType(target.type)
    mutation.reset()
  }

  const setAttr = (key: string, value: string) =>
    setAttrValues((prev) => ({ ...prev, [key]: value }))

  const setDetail = (index: number, patch: Partial<DetailRow>) =>
    setDetails((prev) => prev.map((row, i) => (i === index ? { ...row, ...patch } : row)))
  const addDetail = () => setDetails((prev) => [...prev, { key: '', value: '' }])
  const removeDetail = (index: number) => setDetails((prev) => prev.filter((_, i) => i !== index))

  // Composes the request from the current form state. Only attrs the selected
  // type offers and details with a non-empty key are sent.
  const buildRequest = (fire: boolean) => {
    const attrs: Record<string, string> = {}
    for (const attr of availableAttrs) {
      const value = attrValues[attr.key]
      if (value !== undefined && value !== '') attrs[attr.key] = value
    }
    const detailMap: Record<string, string> = {}
    for (const row of details) {
      if (row.key.trim() !== '') detailMap[row.key] = row.value
    }
    return { source, type, attrs, details: detailMap, description, fire }
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!selectedType) return
    mutation.mutate(buildRequest(false))
  }

  const handleFire = () => {
    setConfirmFire(false)
    mutation.mutate(buildRequest(true))
  }

  // Rule indexes that matched, so the rules table can highlight them. A -1
  // rule_index is the shipped default (no configured row to highlight).
  const matchedIndexes = new Set((result?.matches ?? []).map((m) => m.ruleIndex))

  return (
    <div className="space-y-6">
      <form onSubmit={handleSubmit} className="space-y-6">
        <div className="space-y-2">
          <Label htmlFor="event-type">Event type</Label>
          <Select
            value={selectedId}
            onValueChange={handleEventTypeChange}
            disabled={eventTypesLoading}
          >
            <SelectTrigger id="event-type">
              <SelectValue
                placeholder={eventTypesLoading ? 'Loading event types…' : 'Select an event type'}
              />
            </SelectTrigger>
            <SelectContent>
              {eventTypes.map((t) => {
                const id = eventTypeId(t.source, t.type)
                return (
                  <SelectItem key={id} value={id}>
                    {t.label}
                  </SelectItem>
                )
              })}
            </SelectContent>
          </Select>
          <p className="text-muted-foreground text-xs">
            The kind of event to compose. Attribute inputs below come from this type's schema.
          </p>
        </div>

        <div className="space-y-2">
          <Label htmlFor="description">Description (optional)</Label>
          <Input
            id="description"
            placeholder="Human description shown in the event timeline"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>

        {selectedType && (
          <div className="space-y-3 rounded-md border p-4">
            <div className="space-y-1">
              <Label>Attributes</Label>
              <p className="text-muted-foreground text-xs">
                The matchable values the routing rules evaluate. Leave an attribute blank to omit it
                from the event.
              </p>
            </div>
            {availableAttrs.length === 0 ? (
              <p className="text-muted-foreground text-sm">This event type has no attributes.</p>
            ) : (
              availableAttrs.map((attr) => (
                <div key={attr.key} className="space-y-1.5">
                  <Label htmlFor={`attr-${attr.key}`}>{attr.label}</Label>
                  {attr.key === 'body' ? (
                    <Textarea
                      id={`attr-${attr.key}`}
                      placeholder={attr.placeholder}
                      value={attrValues[attr.key] ?? ''}
                      onChange={(e) => setAttr(attr.key, e.target.value)}
                      rows={3}
                    />
                  ) : (
                    <Input
                      id={`attr-${attr.key}`}
                      placeholder={attr.placeholder}
                      value={attrValues[attr.key] ?? ''}
                      onChange={(e) => setAttr(attr.key, e.target.value)}
                    />
                  )}
                  {attr.help && <p className="text-muted-foreground text-xs">{attr.help}</p>}
                </div>
              ))
            )}
          </div>
        )}

        {selectedType && (
          <div className="space-y-3 rounded-md border border-dashed bg-muted/40 p-4">
            <div className="space-y-1">
              <Label>Details</Label>
              <p className="text-muted-foreground text-xs">
                Source-defined context (e.g. a PR review comment's <code>path</code>,{' '}
                <code>line</code>, <code>diff_hunk</code>). Details are not matched on — they are
                copied verbatim onto the event for rendering.
              </p>
            </div>

            {details.length === 0 ? (
              <p className="text-muted-foreground text-sm">No details.</p>
            ) : (
              <div className="space-y-2">
                {details.map((row, index) => (
                  <div key={index} className="flex flex-wrap items-center gap-2">
                    <Input
                      className="w-40"
                      placeholder="key"
                      value={row.key}
                      onChange={(e) => setDetail(index, { key: e.target.value })}
                      aria-label="Detail key"
                    />
                    <Input
                      className="flex-1 min-w-40"
                      placeholder="value"
                      value={row.value}
                      onChange={(e) => setDetail(index, { value: e.target.value })}
                      aria-label="Detail value"
                    />
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => removeDetail(index)}
                      aria-label="Remove detail"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                ))}
              </div>
            )}

            <Button type="button" variant="outline" size="sm" onClick={addDetail}>
              <Plus className="h-4 w-4" />
              Add detail
            </Button>
          </div>
        )}

        {mutation.error && <div className="text-destructive text-sm">{mutation.error.message}</div>}

        <div className="flex gap-2">
          <Button type="submit" disabled={!selectedType || mutation.isPending}>
            {mutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
            Dry run
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={!selectedType || mutation.isPending}
            onClick={() => setConfirmFire(true)}
          >
            Fire for real
          </Button>
        </div>
      </form>

      {result && <ResultsPanel result={result} orgId={orgId} />}

      <RulesTable rules={rules} eventTypes={eventTypes} matchedIndexes={matchedIndexes} />

      <Dialog open={confirmFire} onOpenChange={setConfirmFire}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Fire this event for real?</DialogTitle>
            <DialogDescription>
              This routes the composed event through the live router: it will wake or create real
              tasks and persist a real event, just like an incoming webhook. This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmFire(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleFire}>
              Fire for real
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function ResultsPanel({ result, orgId }: { result: TestEventResponse; orgId: string }) {
  const matches = result.matches

  return (
    <div className="space-y-3 rounded-md border p-4">
      <div className="flex items-center gap-2">
        <h2 className="text-sm font-semibold">Results</h2>
        {result.fired ? (
          <Badge variant="destructive">Fired</Badge>
        ) : (
          <Badge variant="secondary">Dry run</Badge>
        )}
      </div>

      {matches.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No routing rules matched this event — nothing would be woken or created.
        </p>
      ) : (
        <div className="space-y-3">
          {matches.map((match, i) => (
            <div key={i} className="space-y-2 rounded-md border p-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline">
                  {match.ruleIndex < 0 ? 'Default rule' : `Rule #${match.ruleIndex + 1}`}
                </Badge>
                {match.wouldWake && <Badge variant="secondary">would wake</Badge>}
                {match.wouldCreate && <Badge variant="secondary">would create</Badge>}
              </div>

              {result.fired && match.createdTaskIds.length > 0 && (
                <div className="text-sm">
                  <span className="text-muted-foreground">Created tasks: </span>
                  {match.createdTaskIds.map((id, j) => (
                    <span key={String(id)}>
                      {j > 0 && ', '}
                      <Link
                        to="/tasks/$id"
                        params={{ id: String(id) }}
                        search={{ org: orgId }}
                        className="text-primary hover:underline"
                      >
                        Task {String(id)}
                      </Link>
                    </span>
                  ))}
                </div>
              )}

              {result.fired && match.eventIds.length > 0 && (
                <div className="text-sm">
                  <span className="text-muted-foreground">Events written: </span>
                  {match.eventIds.map((id, j) => (
                    <span key={String(id)}>
                      {j > 0 && ', '}
                      <Link
                        to="/events/$id"
                        params={{ id: String(id) }}
                        search={{ org: orgId }}
                        className="text-primary hover:underline"
                      >
                        Event {String(id)}
                      </Link>
                    </span>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// Compact conditions summary for a rule row, mirroring the routing rules table
// on the events page.
function ruleMatchBadges(rule: RoutingRule): string[] {
  return rule.conditions.map((c) => {
    const op = OP_LABELS[c.op as ConditionOp] ?? c.op
    const value = c.value.length > 40 ? c.value.slice(0, 40) + '…' : c.value
    return `${c.attr} ${op} ${value}`
  })
}

function RulesTable({
  rules,
  eventTypes,
  matchedIndexes,
}: {
  rules: RoutingRule[]
  eventTypes: EventTypeDef[]
  matchedIndexes: Set<number>
}) {
  if (rules.length === 0) {
    return (
      <div className="rounded-md border p-4 text-muted-foreground text-center text-sm">
        No routing rules configured
      </div>
    )
  }

  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-12">#</TableHead>
            <TableHead>Event Type</TableHead>
            <TableHead className="hidden md:table-cell">Filters</TableHead>
            <TableHead>Action</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rules.map((rule, index) => {
            const matched = matchedIndexes.has(index)
            return (
              <TableRow key={index} className={matched ? 'bg-primary/10' : undefined}>
                <TableCell className="font-mono text-muted-foreground">
                  {index + 1}
                  {matched && (
                    <Badge variant="secondary" className="ml-2">
                      matched
                    </Badge>
                  )}
                </TableCell>
                <TableCell className="font-medium whitespace-nowrap">
                  {eventTypeLabel(eventTypes, rule.source, rule.type)}
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
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    {rule.wakeup && <Badge variant="secondary">wake</Badge>}
                    {rule.create && <Badge variant="secondary">create</Badge>}
                    {!rule.wakeup && !rule.create && (
                      <span className="text-muted-foreground whitespace-nowrap">None</span>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}
