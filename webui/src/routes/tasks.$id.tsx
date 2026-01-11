import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  getTaskDetails,
  listLogs,
  updateTask,
  removeEventTask,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Task, TaskLink, Event, LogEntry } from '@/gen/xagent/v1/xagent_pb'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { useState } from 'react'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Textarea } from '@/components/ui/textarea'
import { RelativeTime } from '@/components/ui/relative-time'

export const Route = createFileRoute('/tasks/$id')({
  component: TaskDetail,
})

const statusStyles: Record<string, string> = {
  pending: 'bg-amber-100 text-amber-800 border-amber-200',
  running: 'bg-blue-100 text-blue-800 border-blue-200',
  completed: 'bg-green-100 text-green-800 border-green-200',
  failed: 'bg-red-100 text-red-800 border-red-200',
  cancelled: 'bg-amber-100 text-amber-800 border-amber-200',
  restarting: 'bg-pink-100 text-pink-800 border-pink-200',
  archived: 'bg-gray-100 text-gray-600 border-gray-200',
}

const logTypeStyles: Record<string, string> = {
  llm: 'bg-purple-100 text-purple-800 border-purple-200',
  info: 'bg-blue-100 text-blue-800 border-blue-200',
  error: 'bg-red-100 text-red-800 border-red-200',
}

function StatusBadge({ status }: { status: string }) {
  return (
    <Badge
      variant="outline"
      className={statusStyles[status] ?? 'bg-gray-100 text-gray-600'}
    >
      {status}
    </Badge>
  )
}


function TaskDetail() {
  const { id } = Route.useParams()
  const taskId = BigInt(id)
  const [instruction, setInstruction] = useState('')

  const { data, isLoading, error, refetch } = useQuery(
    getTaskDetails,
    { id: taskId },
    { refetchInterval: 3000 }
  )

  const { data: logsData } = useQuery(
    listLogs,
    { taskId },
    { refetchInterval: 3000 }
  )

  const updateMutation = useMutation(updateTask)
  const removeEventMutation = useMutation(removeEventTask)

  const handleUpdateStatus = async (status: string) => {
    await updateMutation.mutateAsync({ id: taskId, status })
    refetch()
  }

  const handleAddInstruction = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!instruction.trim()) return
    await updateMutation.mutateAsync({
      id: taskId,
      status: 'restarting',
      addInstructions: [{ text: instruction, url: '' }],
    })
    setInstruction('')
    refetch()
  }

  const handleUnlinkEvent = async (eventId: bigint) => {
    await removeEventMutation.mutateAsync({ eventId, taskId })
    refetch()
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

  const isArchived = task.status === 'archived'
  const canCancel = task.status === 'running' || task.status === 'pending'
  const canRestart =
    task.status === 'running' ||
    task.status === 'completed' ||
    task.status === 'failed'
  const canArchive = task.status === 'completed' || task.status === 'failed'

  return (
    <div className="container mx-auto py-8 px-4 space-y-6">
      <Link to="/tasks" className="text-primary hover:underline">
        &larr; Back to Tasks
      </Link>

      {/* Task Header */}
      <Card>
        <CardHeader>
          <div className="flex justify-between items-start">
            <div>
              <CardTitle className="text-2xl">
                {task.name || `Task ${id}`}
              </CardTitle>
              <div className="text-muted-foreground mt-1">
                Workspace: {task.workspace}
              </div>
            </div>
            <div className="flex gap-2">
              {canCancel && (
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => handleUpdateStatus('cancelled')}
                  disabled={updateMutation.isPending}
                >
                  Cancel
                </Button>
              )}
              {canRestart && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleUpdateStatus('restarting')}
                  disabled={updateMutation.isPending}
                >
                  Restart
                </Button>
              )}
              {canArchive && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleUpdateStatus('archived')}
                  disabled={updateMutation.isPending}
                >
                  Archive
                </Button>
              )}
            </div>
          </div>
        </CardHeader>
        <CardContent className="space-y-2">
          {task.parent !== 0n && (
            <div>
              <strong>Parent:</strong>{' '}
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
            <strong>Status:</strong>
            <StatusBadge status={task.status} />
          </div>
          <div className="flex items-center gap-2">
            <strong>Created:</strong>{' '}
            {task.createdAt ? <RelativeTime date={timestampDate(task.createdAt)} /> : '-'}
          </div>
          {task.updatedAt && (
            <div className="flex items-center gap-2">
              <strong>Updated:</strong>{' '}
              <RelativeTime date={timestampDate(task.updatedAt)} />
            </div>
          )}
        </CardContent>
      </Card>

      {/* Instructions */}
      <Card>
        <CardHeader>
          <CardTitle>Instructions</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {task.instructions.length === 0 ? (
            <div className="text-muted-foreground">No instructions</div>
          ) : (
            <div className="space-y-3">
              {task.instructions.map((inst, index) => (
                <InstructionCard key={index} text={inst.text} url={inst.url} />
              ))}
            </div>
          )}
          {!isArchived && (
            <form onSubmit={handleAddInstruction} className="space-y-4 pt-4 border-t">
              <Textarea
                placeholder="Enter a new instruction..."
                value={instruction}
                onChange={(e) => setInstruction(e.target.value)}
                required
              />
              <Button type="submit" disabled={updateMutation.isPending}>
                Add Instruction
              </Button>
            </form>
          )}
        </CardContent>
      </Card>

      {/* Links */}
      {links.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Links</CardTitle>
          </CardHeader>
          <CardContent>
            <LinksSection links={links} />
          </CardContent>
        </Card>
      )}

      {/* Child Tasks */}
      {children.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Child Tasks</CardTitle>
          </CardHeader>
          <CardContent>
            <ChildTasksTable tasks={children} />
          </CardContent>
        </Card>
      )}

      {/* Events */}
      <Card>
        <CardHeader>
          <CardTitle>Events</CardTitle>
        </CardHeader>
        <CardContent>
          {events.length === 0 ? (
            <div className="text-muted-foreground">
              No events linked to this task.
            </div>
          ) : (
            <EventsTable
              events={events}
              onUnlink={handleUnlinkEvent}
              isUnlinking={removeEventMutation.isPending}
            />
          )}
        </CardContent>
      </Card>

      {/* Logs */}
      <Card>
        <CardHeader>
          <CardTitle>Logs</CardTitle>
        </CardHeader>
        <CardContent>
          <LogsTable logs={logs} />
        </CardContent>
      </Card>
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

function ChildTasksTable({ tasks }: { tasks: Task[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Workspace</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Created</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {tasks.map((task) => (
          <TableRow key={String(task.id)}>
            <TableCell>
              <Link
                to="/tasks/$id"
                params={{ id: String(task.id) }}
                className="text-primary hover:underline"
              >
                {task.name || <code className="text-xs">{String(task.id)}</code>}
              </Link>
            </TableCell>
            <TableCell>{task.workspace}</TableCell>
            <TableCell>
              <StatusBadge status={task.status} />
            </TableCell>
            <TableCell className="text-muted-foreground">
              {task.createdAt ? <RelativeTime date={timestampDate(task.createdAt)} /> : '-'}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
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
          <TableHead>URL</TableHead>
          <TableHead></TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {events.map((event) => (
          <TableRow key={String(event.id)}>
            <TableCell>{String(event.id)}</TableCell>
            <TableCell>{event.description}</TableCell>
            <TableCell>
              {event.url && (
                <a
                  href={event.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-primary hover:underline"
                >
                  {event.url}
                </a>
              )}
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
        ))}
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
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
