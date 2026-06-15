import { useMemo, useState } from 'react'
import {
  Bot,
  MessageSquarePlus,
  Play,
  RotateCcw,
  CheckCircle2,
  XCircle,
  Ban,
  Clock,
  Link2,
  Info,
  Archive,
  Bell,
  Zap,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { RelativeTime } from '@/components/relative-time'
import { Markdown, CollapsibleMarkdown } from '@/components/markdown'
import { GithubIcon } from '@/components/github-icon'
import { AtlassianIcon } from '@/components/atlassian-icon'
import type { TimelineItem, LifecycleCategory, ExternalSource } from '@/lib/timeline'

// ----- filtering -------------------------------------------------------------

type FilterKey = 'instruction' | 'agent' | 'external' | 'system'

const FILTERS: { key: FilterKey; label: string }[] = [
  { key: 'agent', label: 'Agent output' },
  { key: 'instruction', label: 'Instructions' },
  { key: 'external', label: 'Events' },
  { key: 'system', label: 'System' },
]

// `system` collapses the lower-signal about-task entries (lifecycle + link).
function filterOf(item: TimelineItem): FilterKey {
  switch (item.kind) {
    case 'instruction':
      return 'instruction'
    case 'report':
      return 'agent'
    case 'external':
      return 'external'
    default:
      return 'system'
  }
}

// ----- top-level component ---------------------------------------------------

export function TaskTimeline({ items }: { items: TimelineItem[] }) {
  const [hidden, setHidden] = useState<Set<FilterKey>>(new Set())

  const visible = useMemo(() => items.filter((i) => !hidden.has(filterOf(i))), [items, hidden])

  const toggle = (key: FilterKey) =>
    setHidden((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })

  if (items.length === 0) {
    return <div className="text-muted-foreground">No activity yet.</div>
  }

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <span className="text-xs text-muted-foreground mr-1">Show</span>
        {FILTERS.map((f) => {
          const active = !hidden.has(f.key)
          return (
            <button
              key={f.key}
              type="button"
              onClick={() => toggle(f.key)}
              className={cn(
                'rounded-full border px-3 py-1 text-xs transition-colors',
                active
                  ? 'bg-foreground text-background border-foreground'
                  : 'bg-transparent text-muted-foreground hover:bg-muted',
              )}
            >
              {f.label}
            </button>
          )
        })}
      </div>

      <ol className="relative space-y-3">
        {/* the rail */}
        <div className="absolute left-[17px] top-2 bottom-2 w-px bg-border" aria-hidden />
        {visible.map((item) => (
          <TimelineRow key={item.id} item={item} />
        ))}
      </ol>
    </div>
  )
}

// ----- marker + row scaffolding ----------------------------------------------

function Marker({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        'relative z-10 flex h-9 w-9 shrink-0 items-center justify-center rounded-full border',
        className,
      )}
    >
      {children}
    </div>
  )
}

function Row({ marker, children }: { marker: React.ReactNode; children: React.ReactNode }) {
  return (
    <li className="relative flex gap-3">
      {marker}
      <div className="min-w-0 flex-1 pt-0.5">{children}</div>
    </li>
  )
}

function TimelineRow({ item }: { item: TimelineItem }) {
  switch (item.kind) {
    case 'instruction':
      return <InstructionRow item={item} />
    case 'external':
      return <ExternalRow item={item} />
    case 'lifecycle':
      return <LifecycleRow item={item} />
    case 'link':
      return <LinkRow item={item} />
    case 'report':
      return <ReportRow item={item} />
  }
}

// ----- per-kind rows ---------------------------------------------------------

function InstructionRow({ item }: { item: Extract<TimelineItem, { kind: 'instruction' }> }) {
  return (
    <Row
      marker={
        <Marker className="border-primary/30 bg-primary/10 text-primary">
          <MessageSquarePlus className="h-4 w-4" />
        </Marker>
      }
    >
      <div className="rounded-lg border border-primary/30 bg-primary/5">
        <div className="flex items-center gap-2 px-4 py-2 text-xs">
          <span className="font-medium text-foreground">Instruction</span>
          {item.wakes && <WakeBadge />}
          <span className="ml-auto text-muted-foreground">
            <RelativeTime date={item.at} />
          </span>
        </div>
        <div className="border-t border-primary/20 px-4 py-3">
          <Markdown text={item.text} />
          {item.url && <SourceLink url={item.url} />}
        </div>
      </div>
    </Row>
  )
}

const externalSource: Record<
  ExternalSource,
  { icon: React.ReactNode; label: string; marker: string }
> = {
  github: {
    icon: <GithubIcon className="h-4 w-4" />,
    label: 'GitHub',
    marker: 'border-slate-300 bg-slate-100 text-slate-800',
  },
  jira: {
    icon: <AtlassianIcon className="h-4 w-4" />,
    label: 'Jira',
    marker: 'border-blue-300 bg-blue-100 text-blue-700',
  },
  other: {
    icon: <Bell className="h-4 w-4" />,
    label: 'External',
    marker: 'border-border bg-muted text-foreground',
  },
}

function ExternalRow({ item }: { item: Extract<TimelineItem, { kind: 'external' }> }) {
  const src = externalSource[item.source]
  return (
    <Row marker={<Marker className={src.marker}>{src.icon}</Marker>}>
      <div className="rounded-lg border border-amber-300/60 bg-amber-50/60 dark:bg-amber-950/20">
        <div className="flex items-center gap-2 px-4 py-2 text-xs">
          <span className="font-medium text-foreground">Event</span>
          <span className="text-muted-foreground">· {src.label}</span>
          {item.wakes && <WakeBadge />}
          <span className="ml-auto text-muted-foreground">
            <RelativeTime date={item.at} />
          </span>
        </div>
        <div className="border-t border-amber-300/40 px-4 py-3">
          <p className="text-sm font-medium text-foreground">{item.description}</p>
          {item.data && <Markdown text={item.data} className="mt-1 text-muted-foreground" />}
          {item.url && <SourceLink url={item.url} />}
        </div>
      </div>
    </Row>
  )
}

const lifecycleConfig: Record<LifecycleCategory, { icon: React.ReactNode; tone: string }> = {
  created: { icon: <Clock className="h-3.5 w-3.5" />, tone: 'text-amber-600' },
  started: { icon: <Play className="h-3.5 w-3.5" />, tone: 'text-blue-600' },
  restarted: { icon: <RotateCcw className="h-3.5 w-3.5" />, tone: 'text-pink-600' },
  completed: { icon: <CheckCircle2 className="h-3.5 w-3.5" />, tone: 'text-green-600' },
  failed: { icon: <XCircle className="h-3.5 w-3.5" />, tone: 'text-red-600' },
  cancelled: { icon: <Ban className="h-3.5 w-3.5" />, tone: 'text-amber-600' },
  archived: { icon: <Archive className="h-3.5 w-3.5" />, tone: 'text-muted-foreground' },
  updated: { icon: <Info className="h-3.5 w-3.5" />, tone: 'text-muted-foreground' },
}

// Lifecycle entries are deliberately understated: a slim inline marker on the
// rail rather than a full card.
function LifecycleRow({ item }: { item: Extract<TimelineItem, { kind: 'lifecycle' }> }) {
  const cfg = lifecycleConfig[item.category]
  return (
    <li className="relative flex items-center gap-3">
      <div
        className={cn(
          'relative z-10 flex h-9 w-9 shrink-0 items-center justify-center rounded-full border bg-muted',
          cfg.tone,
        )}
      >
        {cfg.icon}
      </div>
      <div className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2 text-xs">
        <span className={cn('font-medium', cfg.tone)}>{item.summary}</span>
        <span className="ml-auto text-muted-foreground">
          <RelativeTime date={item.at} />
        </span>
      </div>
    </li>
  )
}

function LinkRow({ item }: { item: Extract<TimelineItem, { kind: 'link' }> }) {
  const icon =
    item.source === 'github' ? (
      <GithubIcon className="h-4 w-4" />
    ) : item.source === 'jira' ? (
      <AtlassianIcon className="h-4 w-4" />
    ) : (
      <Link2 className="h-4 w-4" />
    )
  return (
    <Row marker={<Marker className="border-border bg-muted text-foreground">{icon}</Marker>}>
      <div className="rounded-lg border bg-card px-4 py-3">
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span className="font-medium text-foreground">Link created</span>
          {item.subscribed && (
            <Badge
              variant="outline"
              className="border-blue-200 bg-blue-100 text-blue-800 text-[10px]"
            >
              subscribed
            </Badge>
          )}
          <span className="ml-auto">
            <RelativeTime date={item.at} />
          </span>
        </div>
        <a
          href={item.url}
          target="_blank"
          rel="noopener noreferrer"
          className="mt-1 block text-sm font-medium text-primary hover:underline break-words"
        >
          {item.title}
        </a>
        {item.relevance && <p className="mt-0.5 text-xs text-muted-foreground">{item.relevance}</p>}
      </div>
    </Row>
  )
}

// ----- report row ------------------------------------------------------------

// Reports ARE the agent's output (written via the `report` tool). Each report
// renders as its own entry.
function ReportRow({ item }: { item: Extract<TimelineItem, { kind: 'report' }> }) {
  return (
    <Row
      marker={
        <Marker className="border-violet-300 bg-violet-100 text-violet-700">
          <Bot className="h-4 w-4" />
        </Marker>
      }
    >
      <div className="rounded-lg border bg-card shadow-sm">
        <div className="flex items-center gap-2 px-3 py-2 text-xs">
          <span className="font-medium text-foreground">Agent output</span>
          <span className="ml-auto text-muted-foreground">
            <RelativeTime date={item.at} />
          </span>
        </div>
        <div className="border-t px-4 py-3">
          <CollapsibleMarkdown text={item.content} />
        </div>
      </div>
    </Row>
  )
}

// ----- small shared bits -----------------------------------------------------

function WakeBadge() {
  return (
    <Badge
      variant="outline"
      className="gap-1 border-orange-200 bg-orange-100 text-orange-800 text-[10px]"
    >
      <Zap className="h-3 w-3" />
      woke task
    </Badge>
  )
}

function SourceLink({ url }: { url: string }) {
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      className="mt-2 inline-block break-all text-xs text-muted-foreground hover:text-primary"
    >
      {url}
    </a>
  )
}
