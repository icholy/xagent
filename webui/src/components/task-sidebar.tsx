import { timestampDate } from '@bufbuild/protobuf/wkt'
import type { Duration } from '@bufbuild/protobuf/wkt'
import type { Task, TaskLink } from '@/gen/xagent/v1/xagent_pb'
import type { TaskTab } from '@/lib/task'
import { canArchiveTask, canUnarchiveTask } from '@/lib/task'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { StatusBadge, StatusDot } from '@/components/status-badge'
import { ArchivedBadge } from '@/components/archived-badge'
import { CommandBadge } from '@/components/command-badge'
import { RelativeTime } from '@/components/relative-time'
import { TaskActionsMenu } from '@/components/task-actions-menu'
import { TaskLinks } from '@/components/task-links'
import { useAutoArchiveCountdown } from '@/components/archive-button'
import {
  Archive,
  ArchiveRestore,
  ChevronLeft,
  List,
  Loader2,
  MoreHorizontal,
  Terminal,
} from 'lucide-react'

// TaskSidebar is the task page's left rail: everything about the task that
// isn't the activity itself. It holds the status, title, the view switcher
// (timeline / shell), the metadata details, the links, and the task actions.
// It collapses to an icon rail; on small screens the expanded state overlays
// the main view instead of squeezing it.
export function TaskSidebar({
  task,
  title,
  links,
  collapsed,
  onToggleCollapse,
  tab,
  onTabChange,
  timelineCount,
  shellActive,
  onArchive,
  onUnarchive,
  archivePending,
  onAutoArchiveChange,
  autoArchivePending,
  onCancel,
  cancelPending,
  onRestart,
  restartPending,
}: {
  task: Task
  title: string
  links: TaskLink[]
  collapsed: boolean
  onToggleCollapse: () => void
  tab: TaskTab
  onTabChange: (tab: TaskTab) => void
  timelineCount: number
  shellActive: boolean
  onArchive: () => void
  onUnarchive: () => void
  archivePending: boolean
  onAutoArchiveChange: (autoArchive: Duration) => void
  autoArchivePending?: boolean
  onCancel: () => void
  cancelPending?: boolean
  onRestart: () => void
  restartPending?: boolean
}) {
  return (
    <aside
      className={cn(
        'z-20 flex h-full shrink-0 flex-col overflow-hidden border-r bg-sidebar transition-[width] duration-200',
        collapsed
          ? 'w-16'
          : 'w-[300px] max-md:absolute max-md:inset-y-0 max-md:left-0 max-md:shadow-xl',
      )}
    >
      {/* header row: status + queued command once expanded, then collapse toggle */}
      <div
        className={cn(
          'flex h-12 shrink-0 items-center gap-2.5 border-b',
          collapsed ? 'justify-center' : 'px-3.5',
        )}
      >
        {!collapsed && (
          <span className="flex items-center gap-1.5">
            <StatusBadge task={task} />
            <CommandBadge task={task} />
            <ArchivedBadge task={task} />
          </span>
        )}
        <Button
          variant="outline"
          size="icon-sm"
          className={cn('shrink-0', !collapsed && 'ml-auto')}
          onClick={onToggleCollapse}
          aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          aria-expanded={!collapsed}
        >
          <ChevronLeft
            className={cn('h-4 w-4 transition-transform duration-200', collapsed && 'rotate-180')}
          />
        </Button>
      </div>

      {/* collapsed rail keeps the status visible as a dot */}
      {collapsed && (
        <div className="flex shrink-0 justify-center pb-1 pt-3">
          <StatusDot task={task} />
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden py-3">
        {!collapsed && (
          <div className="px-4 pb-4">
            <h1 className="text-[15px] font-semibold leading-snug text-pretty" title={title}>
              {title}
            </h1>
            {task.namespace && (
              <div className="mt-2 flex flex-wrap items-center gap-1.5">
                <Badge variant="secondary" title="Namespace">
                  {task.namespace}
                </Badge>
              </div>
            )}
          </div>
        )}

        {/* view switcher */}
        <nav className="flex flex-col gap-0.5 px-2">
          <ViewItem
            icon={<List className="h-4 w-4 shrink-0" />}
            label="Timeline"
            active={tab === 'timeline'}
            collapsed={collapsed}
            onClick={() => onTabChange('timeline')}
            badge={timelineCount}
          />
          <ViewItem
            icon={<Terminal className="h-4 w-4 shrink-0" />}
            label="Shell"
            active={tab === 'shell'}
            collapsed={collapsed}
            onClick={() => onTabChange('shell')}
            dot={shellActive}
          />
        </nav>

        {!collapsed && (
          <div className="px-4">
            <div className="my-4 h-px bg-border" />

            {/* details: a compact properties list, label left / value right */}
            <dl className="flex flex-col gap-2">
              <Detail label="Runner">{task.runner}</Detail>
              <Detail label="Workspace">{task.workspace}</Detail>
              <Detail label="Created">
                {task.createdAt ? <RelativeTime date={timestampDate(task.createdAt)} /> : '-'}
              </Detail>
              {task.updatedAt && (
                <Detail label="Updated">
                  <RelativeTime date={timestampDate(task.updatedAt)} />
                </Detail>
              )}
            </dl>

            <div className="my-4 h-px bg-border" />

            {/* links */}
            <div className="mb-2.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Links
            </div>
            <TaskLinks links={links} />
          </div>
        )}
      </div>

      {/* footer actions */}
      <div className="flex shrink-0 flex-col gap-0.5 border-t p-2">
        <ArchiveRow
          task={task}
          collapsed={collapsed}
          onArchive={onArchive}
          onUnarchive={onUnarchive}
          pending={archivePending}
        />
        <TaskActionsMenu
          task={task}
          onAutoArchiveChange={onAutoArchiveChange}
          autoArchivePending={autoArchivePending}
          onCancel={onCancel}
          cancelPending={cancelPending}
          onRestart={onRestart}
          restartPending={restartPending}
          renderTrigger={(pending) => (
            <Button
              variant="ghost"
              disabled={pending}
              title="More actions"
              aria-label="More actions"
              className={cn(footerRowClass, collapsed && 'justify-center px-0')}
            >
              {pending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <MoreHorizontal className="h-4 w-4" />
              )}
              {!collapsed && <span>More actions</span>}
            </Button>
          )}
        />
      </div>
    </aside>
  )
}

// ViewItem is one entry of the sidebar's view switcher: an icon, a label (when
// expanded), and either a count badge (timeline) or an activity dot (shell).
function ViewItem({
  icon,
  label,
  active,
  collapsed,
  onClick,
  badge,
  dot,
}: {
  icon: React.ReactNode
  label: string
  active: boolean
  collapsed: boolean
  onClick: () => void
  badge?: number
  dot?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      aria-current={active ? 'true' : undefined}
      className={cn(
        'relative flex h-9 cursor-pointer items-center gap-3 rounded-lg px-3 text-sm font-medium transition-colors',
        active
          ? 'bg-accent text-accent-foreground'
          : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground',
        collapsed && 'justify-center px-0',
      )}
    >
      {icon}
      {!collapsed && <span className="min-w-0 flex-1 truncate text-left">{label}</span>}
      {!collapsed && badge !== undefined && badge > 0 && (
        <span
          className={cn(
            'rounded-full px-1.5 py-0.5 text-xs font-semibold leading-none',
            active ? 'bg-foreground text-background' : 'bg-muted text-muted-foreground',
          )}
        >
          {badge}
        </span>
      )}
      {dot && (
        <span
          className={cn(
            'h-2 w-2 shrink-0 rounded-full bg-green-500',
            collapsed && 'absolute right-1.5 top-1.5',
          )}
          aria-label="Shell session active"
        />
      )}
    </button>
  )
}

// Detail is one row of the sidebar's properties list: a muted label on the
// left, the value right-aligned on the same line. Long values (workspace or
// runner names) truncate rather than wrap, with the full text in the tooltip.
function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  const title = typeof children === 'string' ? children : undefined
  return (
    <div className="flex items-center justify-between gap-4 text-sm">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 truncate text-right font-medium" title={title}>
        {children}
      </dd>
    </div>
  )
}

const footerRowClass = 'h-9 w-full justify-start gap-3 px-3 font-medium text-muted-foreground'

// ArchiveRow is the sidebar's archive/unarchive action. Same semantics as
// ArchiveButton (icon flips with the available action, auto-archive countdown
// stays visible, greys out when neither action is available) restyled as a
// full-width footer row.
function ArchiveRow({
  task,
  collapsed,
  onArchive,
  onUnarchive,
  pending,
}: {
  task: Task
  collapsed: boolean
  onArchive: () => void
  onUnarchive: () => void
  pending: boolean
}) {
  const countdown = useAutoArchiveCountdown(task)
  const unarchive = canUnarchiveTask(task)
  const canAct = canArchiveTask(task) || unarchive
  const label = unarchive ? 'Unarchive' : 'Archive'
  const Icon = unarchive ? ArchiveRestore : Archive
  return (
    <Button
      variant="ghost"
      onClick={unarchive ? onUnarchive : onArchive}
      disabled={pending || !canAct}
      title={countdown ? `Auto-archives in ${countdown}` : `${label} task`}
      aria-label={`${label} task`}
      className={cn(footerRowClass, collapsed && 'justify-center px-0')}
    >
      {pending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Icon className="h-4 w-4" />}
      {!collapsed && <span>{label}</span>}
      {!collapsed && countdown && (
        <span className="ml-auto text-xs tabular-nums text-muted-foreground">{countdown}</span>
      )}
    </Button>
  )
}
