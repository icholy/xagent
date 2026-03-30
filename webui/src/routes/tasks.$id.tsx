import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  getTaskDetails,
  listLogs,
  updateTask,
  removeEventTask,
  archiveTask,
  unarchiveTask,
  cancelTask,
  restartTask,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Task, TaskLink, Event, LogEntry } from '@/gen/xagent/v1/xagent_pb'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { useState } from 'react'
import { canArchiveTask, canUnarchiveTask, canCancelTask, canRestartTask, isArchivedTask } from '@/lib/task'
import { ArchivedBadge } from '@/components/archived-badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { RelativeTime } from '@/components/relative-time'
import { CommandBadge } from '@/components/command-badge'
import { Plus, Loader2 } from 'lucide-react'

export const Route = createFileRoute('/tasks/$id')({
  staticData: { orgSwitchRedirect: '/tasks' },
  component: TaskDetail,
})

const logTypeStyles: Record<string, string> = {
  llm: 'bg-purple-100 text-purple-800 border-purple-200',
  info: 'bg-blue-100 text-blue-800 border-blue-200',
  error: 'bg-red-100 text-red-800 border-red-200',
  audit: 'bg-yellow-100 text-yellow-800 border-yellow-200',
}


function TaskDetail() {
  const { id } = Route.useParams()
  const taskId = BigInt(id)
  const [instruction, setInstruction] = useState('')

  const { data, isLoading, error, refetch } = useQuery(
    getTaskDetails,
    { id: taskId },
    { refetchInterval: 6000 }
  )

  const { data: logsData } = useQuery(
    listLogs,
    { taskId },
    { refetchInterval: 6000 }
  )

  const updateMutation = useMutation(updateTask, { onSuccess: () => refetch() })
  const removeEventMutation = useMutation(removeEventTask, { onSuccess: () => refetch() })
  const archiveMutation = useMutation(archiveTask, { onSuccess: () => refetch() })
  const unarchiveMutation = useMutation(unarchiveTask, { onSuccess: () => refetch() })
  const cancelMutation = useMutation(cancelTask, { onSuccess: () => refetch() })
  const restartMutation = useMutation(restartTask, { onSuccess: () => refetch() })

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

  const handleUnlinkEvent = async (eventId: bigint) => {
    await removeEventMutation.mutateAsync({ eventId, taskId })
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
  const children = data?.children ?? []
  const events = data?.events ?? []
  const links = data?.links ?? []
  const logs = logsData?.entries ?? []

  if (!task) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Task not found</div>
      </div>
    )
  }

  const isMutating = archiveMutation.isPending || unarchiveMutation.isPending || cancelMutation.isPending || restartMutation.isPending

  return (
    <div className="container mx-auto py-8 px-4 space-y-6">
      <div className="flex flex-wrap justify-between items-start gap-4 mb-6">
        <h1 className="text-2xl font-bold">{task.name || `Unnamed - ${id}`}</h1>
        <div className="flex flex-wrap gap-2">
          {canCancelTask(task) && (
            <Button
              variant="destructive"
              size="sm"
              onClick={handleCancel}
              disabled={isMutating}
            >
              {cancelMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Cancel
            </Button>
          )}
          {canRestartTask(task) && (
            <Button
              variant="outline"
              size="sm"
              onClick={handleRestart}
              disabled={isMutating}
            >
              {restartMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Restart
            </Button>
          )}
          {canArchiveTask(task) && (
            <Button
              variant="outline"
              size="sm"
              onClick={handleArchive}
              disabled={isMutating}
            >
              {archiveMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Archive
            </Button>
          )}
          {canUnarchiveTask(task) && (
            <Button
              variant="outline"
              size="sm"
              onClick={handleUnarchive}
              disabled={isMutating}
            >
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
          {task.parent !== 0n && (
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Parent:</span>
              <Link
                to="/tasks/$id"
                params={{ id: String(task.parent) }}
                className="text-primary hover:underline"
              >
                Task {String(task.parent)}
              </Link>
            </div>
          )}
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Status:</span>
            <StatusBadge task={task} />
            <CommandBadge task={task} />
            <ArchivedBadge task={task} />
          </div>
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground">Created:</span>
            <span>{task.createdAt ? <RelativeTime date={timestampDate(task.createdAt)} /> : '-'}</span>
          </div>
          {task.updatedAt && (
            <div className="flex items-center gap-2">
              <span className="text-muted-foreground">Updated:</span>
              <span><RelativeTime date={timestampDate(task.updatedAt)} /></span>
            </div>
          )}
        </div>
      </div>

      {/* Links */}
      {links.length > 0 && (
        <div className="rounded-lg border p-6">
          <h2 className="text-lg font-semibold mb-4">Links</h2>
          <LinksSection links={links} />
        </div>
      )}

      {/* Instructions */}
      <div className="rounded-lg border p-6">
        <h2 className="text-lg font-semibold mb-4">Instructions</h2>
        {task.instructions.length === 0 ? (
          <p className="text-muted-foreground">No instructions</p>
        ) : (
          <div className="space-y-3">
            {task.instructions.map((inst, index) => (
              <InstructionCard key={index} text={inst.text} url={inst.url} />
            ))}
          </div>
        )}
        {!isArchivedTask(task) && (
          <form onSubmit={handleAddInstruction} className="space-y-4 pt-4 mt-4 border-t">
            <Textarea
              placeholder="Enter a new instruction..."
              value={instruction}
              onChange={(e) => setInstruction(e.target.value)}
              required
            />
            <div className="flex justify-start">
              <Button type="submit" disabled={updateMutation.isPending}>
                {updateMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
                Instruction
              </Button>
            </div>
          </form>
        )}
      </div>

      {/* Child Tasks */}
      {children.length > 0 && (
        <div className="rounded-lg border p-6">
          <h2 className="text-lg font-semibold mb-4">Child Tasks</h2>
          <ChildTasksTable tasks={children} onUpdate={refetch} />
        </div>
      )}

      {/* Events */}
      {events.length > 0 && (
        <div className="rounded-lg border p-6">
          <h2 className="text-lg font-semibold mb-4">Events</h2>
          <EventsTable
            events={events}
            onUnlink={handleUnlinkEvent}
            isUnlinking={removeEventMutation.isPending}
          />
        </div>
      )}

      {/* Logs */}
      <div className="rounded-lg border p-6">
        <h2 className="text-lg font-semibold mb-4">Logs</h2>
        <LogsTable logs={logs} />
      </div>
    </div>
  )
}

function InstructionCard({ text, url }: { text: string; url: string }) {
  return (
    <div className="bg-muted/50 border rounded-lg p-4">
      <div className="whitespace-pre-wrap break-words text-foreground">
        {text}
      </div>
      {url && (
        <a
          href={url}
          target="_blank"
          rel="noopener noreferrer"
          className="text-sm text-muted-foreground hover:text-primary mt-2 inline-block break-all"
        >
          {url}
        </a>
      )}
    </div>
  )
}

function LinksSection({ links }: { links: TaskLink[] }) {
  return (
    <ul className="space-y-2">
      {links.map((link) => (
        <li key={String(link.id)} className="flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <a
              href={link.url}
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary hover:underline"
            >
              {link.title || link.url}
            </a>
            {link.notify && (
              <Badge
                variant="outline"
                className="bg-blue-100 text-blue-800 border-blue-200 text-xs"
              >
                notify
              </Badge>
            )}
          </div>
          {link.relevance && (
            <span className="text-sm text-muted-foreground">
              {link.relevance}
            </span>
          )}
        </li>
      ))}
    </ul>
  )
}

function ChildTasksTable({ tasks, onUpdate }: { tasks: Task[]; onUpdate: () => void }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Runner</TableHead>
          <TableHead>Workspace</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Created</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {tasks.map((task) => (
          <ChildTaskRow key={String(task.id)} task={task} onUpdate={onUpdate} />
        ))}
      </TableBody>
    </Table>
  )
}

function ChildTaskRow({ task, onUpdate }: { task: Task; onUpdate: () => void }) {
  const archiveMutation = useMutation(archiveTask, { onSuccess: () => onUpdate() })

  const handleArchive = async () => {
    await archiveMutation.mutateAsync({ id: task.id })
  }

  return (
    <TableRow>
      <TableCell>
        <Link
          to="/tasks/$id"
          params={{ id: String(task.id) }}
          className="text-primary hover:underline"
        >
          {task.name || `Unnamed - ${task.id}`}
        </Link>
      </TableCell>
      <TableCell>{task.runner}</TableCell>
      <TableCell>{task.workspace}</TableCell>
      <TableCell>
        <span className="flex items-center gap-2">
          <StatusBadge task={task} />
          <CommandBadge task={task} />
        </span>
      </TableCell>
      <TableCell className="text-muted-foreground">
        {task.createdAt ? <RelativeTime date={timestampDate(task.createdAt)} /> : '-'}
      </TableCell>
      <TableCell>
        {canArchiveTask(task) && (
          <Button
            variant="outline"
            size="sm"
            onClick={handleArchive}
            disabled={archiveMutation.isPending}
          >
            {archiveMutation.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            Archive
          </Button>
        )}
      </TableCell>
    </TableRow>
  )
}

function EventsTable({
  events,
  onUnlink,
  isUnlinking,
}: {
  events: Event[]
  onUnlink: (eventId: bigint) => void
  isUnlinking: boolean
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>ID</TableHead>
          <TableHead>Description</TableHead>
          <TableHead>Data</TableHead>
          <TableHead>Created</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {events.map((event) => {
          const dataContent = event.data || '-'
          const truncatedData = dataContent.length > 100 ? dataContent.slice(0, 100) + '...' : dataContent

          return (
            <TableRow key={String(event.id)}>
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
              <TableCell className="text-muted-foreground">
                {event.createdAt ? <RelativeTime date={timestampDate(event.createdAt)} /> : '-'}
              </TableCell>
              <TableCell>
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => onUnlink(event.id)}
                  disabled={isUnlinking}
                >
                  Remove
                </Button>
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}

function LogsTable({ logs }: { logs: LogEntry[] }) {
  if (logs.length === 0) {
    return <div className="text-muted-foreground">No logs yet.</div>
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Type</TableHead>
          <TableHead>Content</TableHead>
          <TableHead>Created</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {logs.map((log, index) => (
          <TableRow key={index}>
            <TableCell>
              <Badge
                variant="outline"
                className={
                  logTypeStyles[log.type] ?? 'bg-gray-100 text-gray-600'
                }
              >
                {log.type}
              </Badge>
            </TableCell>
            <TableCell className="whitespace-pre-wrap break-words">
              {log.content}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {log.createdAt ? <RelativeTime date={timestampDate(log.createdAt)} /> : '-'}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
