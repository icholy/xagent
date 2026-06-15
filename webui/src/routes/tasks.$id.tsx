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
import { useState } from 'react'
import {
  canArchiveTask,
  canUnarchiveTask,
  canCancelTask,
  canRestartTask,
  isArchivedTask,
} from '@/lib/task'
import { eventsToTimeline } from '@/lib/timeline'
import { ArchivedBadge } from '@/components/archived-badge'
import { ArchiveButton } from '@/components/archive-button'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { RelativeTime } from '@/components/relative-time'
import { CommandBadge } from '@/components/command-badge'
import { TaskTimeline } from '@/components/task-timeline'
import { Plus, Loader2 } from 'lucide-react'

export const Route = createFileRoute('/tasks/$id')({
  staticData: { orgSwitchRedirect: '/tasks' },
  component: TaskDetail,
})

function TaskDetail() {
  const { id } = Route.useParams()
  const taskId = BigInt(id)
  const [instruction, setInstruction] = useState('')

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

  const handleAddInstruction = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!instruction.trim()) return
    await updateMutation.mutateAsync({
      id: taskId,
      start: true,
      addInstructions: [{ text: instruction, url: '' }],
    })
    setInstruction('')
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
        <div className="flex flex-wrap gap-2">
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

      {/* Task Details */}
      <div className="rounded-lg border p-4">
        <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Runner:</span>
            <span>{task.runner}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Workspace:</span>
            <span>{task.workspace}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Status:</span>
            <StatusBadge task={task} />
            <CommandBadge task={task} />
            <ArchivedBadge task={task} />
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
      </div>

      {/* Activity timeline */}
      <div className="rounded-lg border p-6">
        <h2 className="text-lg font-semibold mb-4">Activity</h2>
        <TaskTimeline items={timeline} />
      </div>

      {/* Add instruction */}
      {!isArchivedTask(task) && (
        <div className="rounded-lg border p-6">
          <form onSubmit={handleAddInstruction} className="space-y-4">
            <Textarea
              placeholder="Enter a new instruction..."
              value={instruction}
              onChange={(e) => setInstruction(e.target.value)}
              required
            />
            <div className="flex justify-start">
              <Button type="submit" disabled={updateMutation.isPending}>
                {updateMutation.isPending ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Plus className="h-4 w-4" />
                )}
                Instruction
              </Button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}
