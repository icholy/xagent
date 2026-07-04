import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  getTaskDetails,
  listEventsByTask,
  updateTask,
  archiveTask,
  unarchiveTask,
  cancelTask,
  restartTask,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { useState, useRef, useLayoutEffect } from 'react'
import {
  canArchiveTask,
  canUnarchiveTask,
  canCancelTask,
  canRestartTask,
  canOpenShell,
  isArchivedTask,
} from '@/lib/task'
import { eventsToTimeline } from '@/lib/timeline'
import { useOrgId } from '@/hooks/use-org-id'
import { useShellState } from '@/hooks/use-shell-state'
import { isShellActive } from '@/lib/shell-sessions'
import { cn } from '@/lib/utils'
import { ArchivedBadge } from '@/components/archived-badge'
import { ArchiveButton } from '@/components/archive-button'
import { AutoArchiveControl } from '@/components/auto-archive-control'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { RelativeTime } from '@/components/relative-time'
import { CommandBadge } from '@/components/command-badge'
import { TaskTimeline } from '@/components/task-timeline'
import { TaskShellPanel } from '@/components/task-shell-panel'
import { TaskLinksTab } from '@/components/task-links'
import { Send, Loader2, List, Terminal, Link2 } from 'lucide-react'

export const Route = createFileRoute('/tasks/$id')({
  staticData: { orgSwitchRedirect: '/tasks' },
  component: TaskDetail,
})

function TaskDetail() {
  const { id } = Route.useParams()
  const taskId = BigInt(id)
  const orgId = useOrgId()
  // A shell session opened in this tab keeps the task in "running"; track it so
  // the Shell button stays reachable (and marked active) despite that status.
  const shellActive = isShellActive(useShellState(String(taskId)).phase)
  const [tab, setTab] = useState<TabKey>('timeline')
  const [instruction, setInstruction] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Auto-grow the composer to fit its content. Done in JS rather than relying on
  // CSS field-sizing, which isn't supported across all browsers (e.g. Firefox).
  // The min/max heights are enforced by the textarea's CSS classes.
  useLayoutEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    const borderY = el.offsetHeight - el.clientHeight
    el.style.height = `${el.scrollHeight + borderY}px`
  }, [instruction])

  const { data, isLoading, error, refetch } = useQuery(
    getTaskDetails,
    { id: taskId },
    { refetchInterval: 60000 },
  )

  // The single activity view is the timeline: every instruction, external
  // event, report, lifecycle transition, and link the task produced, in order.
  const { data: eventsData, refetch: refetchEvents } = useQuery(
    listEventsByTask,
    { taskId },
    { refetchInterval: 60000 },
  )

  const refetchAll = () => {
    refetch()
    refetchEvents()
  }

  const updateMutation = useMutation(updateTask, { onSuccess: refetchAll })
  const autoArchiveMutation = useMutation(updateTask, { onSuccess: refetchAll })
  const archiveMutation = useMutation(archiveTask, { onSuccess: refetchAll })
  const unarchiveMutation = useMutation(unarchiveTask, { onSuccess: refetchAll })
  const cancelMutation = useMutation(cancelTask, { onSuccess: refetchAll })
  const restartMutation = useMutation(restartTask, { onSuccess: refetchAll })

  const handleArchive = async () => {
    await archiveMutation.mutateAsync({ id: taskId })
  }

  const handleUnarchive = async () => {
    await unarchiveMutation.mutateAsync({ id: taskId })
  }

  const handleCancel = async () => {
    await cancelMutation.mutateAsync({ id: taskId })
  }

  const handleRestart = async () => {
    await restartMutation.mutateAsync({ id: taskId })
  }

  const submitInstruction = async () => {
    if (!instruction.trim() || updateMutation.isPending) return
    await updateMutation.mutateAsync({
      id: taskId,
      start: true,
      addInstructions: [{ text: instruction, url: '' }],
    })
    setInstruction('')
  }

  const handleAddInstruction = (e: React.FormEvent) => {
    e.preventDefault()
    submitInstruction()
  }

  // Enter sends, Shift+Enter inserts a newline (chat-style).
  const handleInstructionKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      submitInstruction()
    }
  }

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Loading task...</div>
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

  const task = data?.task
  const links = data?.links ?? []
  const timeline = eventsToTimeline(eventsData?.events ?? [])

  if (!task) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Task not found</div>
      </div>
    )
  }

  const isMutating =
    archiveMutation.isPending ||
    unarchiveMutation.isPending ||
    cancelMutation.isPending ||
    restartMutation.isPending

  return (
    <div className="container mx-auto py-8 px-4 space-y-6">
      <div className="flex flex-wrap justify-between items-start gap-4 mb-6">
        <h1 className="text-2xl font-bold">{task.name || `Unnamed - ${id}`}</h1>
        <div className="flex flex-wrap items-center gap-2">
          <AutoArchiveControl
            task={task}
            onChange={(autoArchive) => autoArchiveMutation.mutateAsync({ id: taskId, autoArchive })}
            pending={autoArchiveMutation.isPending}
            disabled={isArchivedTask(task)}
          />
          {canCancelTask(task) && (
            <Button variant="destructive" size="sm" onClick={handleCancel} disabled={isMutating}>
              {cancelMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Cancel
            </Button>
          )}
          {canRestartTask(task) && (
            <Button variant="outline" size="sm" onClick={handleRestart} disabled={isMutating}>
              {restartMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Restart
            </Button>
          )}
          {canArchiveTask(task) && (
            <ArchiveButton
              task={task}
              onArchive={handleArchive}
              pending={archiveMutation.isPending}
              disabled={isMutating}
            />
          )}
          {canUnarchiveTask(task) && (
            <Button variant="outline" size="sm" onClick={handleUnarchive} disabled={isMutating}>
              {unarchiveMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Unarchive
            </Button>
          )}
        </div>
      </div>

      {/* Details + activity in a single card: metadata header strip, an in-page
          tab bar, then the selected view (timeline / shell / links). */}
      <div className="overflow-hidden rounded-lg border">
        <div className="flex flex-wrap items-center gap-x-6 gap-y-2 border-b p-4 text-sm">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Status:</span>
            <StatusBadge task={task} />
            <CommandBadge task={task} />
            <ArchivedBadge task={task} />
          </div>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Runner:</span>
            <span>{task.runner}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Workspace:</span>
            <span>{task.workspace}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Created:</span>
            <span>
              {task.createdAt ? <RelativeTime date={timestampDate(task.createdAt)} /> : '-'}
            </span>
          </div>
          {task.updatedAt && (
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Updated:</span>
              <span>
                <RelativeTime date={timestampDate(task.updatedAt)} />
              </span>
            </div>
          )}
        </div>

        {/* In-page tab bar: switch between the timeline, the debug shell, and
            the task's links without leaving the page. */}
        <div className="flex items-center gap-1 border-b px-2">
          <TabButton
            active={tab === 'timeline'}
            onClick={() => setTab('timeline')}
            icon={<List className="h-4 w-4" />}
            label="Timeline"
            count={timeline.length}
          />
          <TabButton
            active={tab === 'shell'}
            onClick={() => setTab('shell')}
            icon={<Terminal className="h-4 w-4" />}
            label="Shell"
            dot={shellActive}
          />
          <TabButton
            active={tab === 'links'}
            onClick={() => setTab('links')}
            icon={<Link2 className="h-4 w-4" />}
            label="Links"
            count={links.length}
          />
        </div>

        {tab === 'timeline' && (
          <>
            <div className="p-6">
              <TaskTimeline items={timeline} />
            </div>

            {/* Add instruction */}
            {!isArchivedTask(task) && (
              <div className="border-t p-4">
                <form onSubmit={handleAddInstruction} className="flex items-end gap-2">
                  <Textarea
                    ref={textareaRef}
                    placeholder="Send an instruction…  (Enter to send, Shift+Enter for newline)"
                    value={instruction}
                    onChange={(e) => setInstruction(e.target.value)}
                    onKeyDown={handleInstructionKeyDown}
                    rows={1}
                    className="max-h-60 min-h-[40px] flex-1 resize-none overflow-y-auto"
                    required
                  />
                  <Button
                    type="submit"
                    size="icon"
                    disabled={updateMutation.isPending}
                    title="Send instruction"
                  >
                    {updateMutation.isPending ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <Send className="h-4 w-4" />
                    )}
                  </Button>
                </form>
              </div>
            )}
          </>
        )}

        {tab === 'shell' && (
          <TaskShellPanel taskId={taskId} orgId={orgId} canOpen={canOpenShell(task)} />
        )}

        {tab === 'links' && <TaskLinksTab links={links} />}
      </div>
    </div>
  )
}

type TabKey = 'timeline' | 'shell' | 'links'

// TabButton is one entry in the in-page tab bar: an underline-style tab with an
// icon, a label, and either a count badge or a "session active" dot.
function TabButton({
  active,
  onClick,
  icon,
  label,
  count,
  dot,
}: {
  active: boolean
  onClick: () => void
  icon: React.ReactNode
  label: string
  count?: number
  dot?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-selected={active}
      className={cn(
        // -mb-px pulls the active underline onto the bar's own bottom border.
        'relative -mb-px flex items-center gap-2 border-b-2 px-3 py-3 text-sm font-medium transition-colors',
        active
          ? 'border-primary text-foreground'
          : 'border-transparent text-muted-foreground hover:text-foreground',
      )}
    >
      {icon}
      {label}
      {dot && (
        <span className="h-2 w-2 rounded-full bg-green-500" aria-label="Shell session active" />
      )}
      {count !== undefined && count > 0 && (
        <span
          className={cn(
            'rounded-full px-2 py-0.5 text-xs font-medium',
            active ? 'bg-foreground text-background' : 'bg-muted text-muted-foreground',
          )}
        >
          {count}
        </span>
      )}
    </button>
  )
}
