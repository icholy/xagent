import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  getTaskDetails,
  updateTask,
  archiveTask,
  unarchiveTask,
  cancelTask,
  restartTask,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { useState, useRef, useLayoutEffect } from 'react'
import type { TaskTab } from '@/lib/task'
import { toTaskTab } from '@/lib/task'
import { canOpenShell, isArchivedTask } from '@/lib/task'
import { useTaskTimeline } from '@/hooks/use-task-timeline'
import { useOrgId } from '@/hooks/use-org-id'
import { useShellState } from '@/hooks/use-shell-state'
import { isShellActive } from '@/lib/shell-sessions'
import { cn } from '@/lib/utils'
import { ArchivedBadge } from '@/components/archived-badge'
import { ArchiveButton } from '@/components/archive-button'
import { TaskActionsMenu } from '@/components/task-actions-menu'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { RelativeTime } from '@/components/relative-time'
import { CommandBadge } from '@/components/command-badge'
import { TaskTimeline } from '@/components/task-timeline'
import { TaskShellPanel } from '@/components/task-shell-panel'
import { TaskLinksTab } from '@/components/task-links'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Send, Loader2, List, Terminal, Link2 } from 'lucide-react'

export const Route = createFileRoute('/tasks/$id')({
  staticData: { orgSwitchRedirect: '/tasks' },
  // The active panel is mirrored in ?tab= so shell/links can be deep-linked. The
  // default "timeline" is left out of the URL to keep the common link clean.
  validateSearch: (search: Record<string, unknown>): { tab?: 'shell' | 'links' } => {
    const tab = toTaskTab(search.tab)
    return tab === 'timeline' ? {} : { tab }
  },
  component: TaskDetail,
})

function TaskDetail() {
  const { id } = Route.useParams()
  const taskId = BigInt(id)
  const orgId = useOrgId()
  const navigate = useNavigate()
  // A shell session opened in this tab keeps the task in "running"; track it so
  // the Shell button stays reachable (and marked active) despite that status.
  const shellActive = isShellActive(useShellState(String(taskId)).phase)
  // The active tab lives in the URL (?tab=) so panels can be deep-linked. The
  // default "timeline" is stored as an absent param, so strip it when switching.
  const tab = toTaskTab(Route.useSearch().tab)
  const setTab = (next: TaskTab) =>
    navigate({
      to: '/tasks/$id',
      params: { id },
      search: (prev) => ({ ...prev, tab: next === 'timeline' ? undefined : next }),
      replace: true,
    })
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
  // It is a bidirectional infinite query — opens at the tail, loads older pages
  // on demand, and follows the tail on each SSE task_logs signal.
  const {
    timeline,
    follow: followTimeline,
    loadOlder,
    hasOlder,
    isLoadingOlder,
  } = useTaskTimeline(taskId)

  const refetchAll = () => {
    refetch()
    // A mutation may append a timeline event (a new instruction, a lifecycle
    // transition); pull just the newer page so it shows without a full refetch.
    followTimeline()
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

  if (!task) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Task not found</div>
      </div>
    )
  }

  return (
    <div className="container mx-auto py-8 px-4 space-y-6">
      {/* On small screens the controls wrap onto their own line below the
          title (flex-wrap). On md+ we switch to flex-nowrap and truncate the
          title instead, so the whole header stays on a single line with the
          controls pinned to the right. */}
      <div className="flex flex-wrap md:flex-nowrap justify-between items-start gap-4 mb-6">
        {/* title shows the full name on hover, useful once it's truncated. */}
        <h1
          className="text-2xl font-bold md:min-w-0 md:truncate"
          title={task.name || `Unnamed - ${id}`}
        >
          {task.name || `Unnamed - ${id}`}
        </h1>
        <div className="flex items-center gap-2 flex-shrink-0">
          {/* The panel switcher rides in the header, right beside the actions
              menu, so timeline / shell / links stay reachable from the top of
              the page. The active tab is still mirrored in ?tab= for deep links. */}
          <Tabs value={tab} onValueChange={(value) => setTab(value as TaskTab)}>
            <TabsList>
              <TabsTrigger value="timeline">
                <List className="h-4 w-4" />
                Timeline
                <TabCount active={tab === 'timeline'} count={timeline.length} />
              </TabsTrigger>
              <TabsTrigger value="shell">
                <Terminal className="h-4 w-4" />
                Shell
                {shellActive && (
                  <span
                    className="h-2 w-2 rounded-full bg-green-500"
                    aria-label="Shell session active"
                  />
                )}
              </TabsTrigger>
              <TabsTrigger value="links">
                <Link2 className="h-4 w-4" />
                Links
                <TabCount active={tab === 'links'} count={links.length} />
              </TabsTrigger>
            </TabsList>
          </Tabs>
          {/* Archive sits just left of the overflow menu as its own icon button:
              it's the most common single-click action, so it stays one tap away
              rather than buried in the menu. The icon flips between archive and
              restore to match whichever action the server exposes, and it greys
              out (rather than disappearing) when neither is available so the
              header layout doesn't shift as the task changes state. */}
          <ArchiveButton
            task={task}
            onArchive={handleArchive}
            onUnarchive={handleUnarchive}
            pending={archiveMutation.isPending || unarchiveMutation.isPending}
          />
          <TaskActionsMenu
            task={task}
            onAutoArchiveChange={(autoArchive) =>
              autoArchiveMutation.mutateAsync({ id: taskId, autoArchive })
            }
            autoArchivePending={autoArchiveMutation.isPending}
            onCancel={handleCancel}
            cancelPending={cancelMutation.isPending}
            onRestart={handleRestart}
            restartPending={restartMutation.isPending}
          />
        </div>
      </div>

      {/* Details + activity in a single card: a metadata header strip followed
          by the selected view (timeline / shell / links). The tab switcher that
          picks the view lives up in the page header, beside the actions menu. */}
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

        {tab === 'timeline' && (
          <>
            <div className="p-6">
              {/* Scroll-back into history. The timeline opens at the newest
                  page; this pulls older pages until the first event is
                  reached, at which point prev_page_token empties and the
                  button disappears. */}
              {hasOlder && (
                <div className="mb-4 flex justify-center">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => loadOlder()}
                    disabled={isLoadingOlder}
                  >
                    {isLoadingOlder ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Load older'}
                  </Button>
                </div>
              )}
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
                    size="icon-lg"
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

// TabCount is the small pill after a tab's label showing how many items sit
// behind that panel (timeline entries, links). It flips to a solid fill when its
// tab is active so it stays legible against the highlighted trigger; a zero
// count renders nothing.
function TabCount({ active, count }: { active: boolean; count: number }) {
  if (count <= 0) return null
  return (
    <span
      className={cn(
        'rounded-full px-1.5 py-0.5 text-xs font-medium leading-none',
        active ? 'bg-foreground text-background' : 'bg-background text-muted-foreground',
      )}
    >
      {count}
    </span>
  )
}
