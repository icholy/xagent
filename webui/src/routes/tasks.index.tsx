import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listTasks, archiveTask } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Task } from '@/gen/xagent/v1/xagent_pb'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { canArchiveTask, isChildTask } from '@/lib/task'
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
import { Switch } from '@/components/ui/switch'
import { RelativeTime } from '@/components/ui/relative-time'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'
import { Plus } from 'lucide-react'

export const Route = createFileRoute('/tasks/')({
  component: TasksPage,
})

function TasksPage() {
  const [showChildTasks, setShowChildTasks] = useState(() => {
    const stored = localStorage.getItem('showChildTasks')
    return stored !== null ? stored === 'true' : false
  })

  const { data, isLoading, error, refetch } = useQuery(listTasks, {}, {
    refetchInterval: 3000,
  })

  const handleToggleChildTasks = (checked: boolean) => {
    setShowChildTasks(checked)
    localStorage.setItem('showChildTasks', String(checked))
  }

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Loading tasks...</div>
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

  const allTasks = data?.tasks ?? []
  const tasks = showChildTasks
    ? allTasks
    : allTasks.filter((task) => task.parent === 0n)
  const hiddenCount = allTasks.length - tasks.length

  return (
    <div className="container mx-auto py-8 px-4">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Tasks</h1>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <Label htmlFor="show-child-tasks" className="text-sm text-muted-foreground cursor-pointer">
              Show child tasks{hiddenCount > 0 && !showChildTasks && ` (${hiddenCount} hidden)`}
            </Label>
            <Switch
              id="show-child-tasks"
              checked={showChildTasks}
              onCheckedChange={handleToggleChildTasks}
            />
          </div>
          <Link to="/tasks/new">
            <Button>
              <Plus className="h-4 w-4" />
              Task
            </Button>
          </Link>
        </div>
      </div>
      {tasks.length === 0 ? (
        <div className="text-muted-foreground text-center py-8">
          No tasks found
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Workspace</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Created</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tasks.map((task) => (
              <TaskRow key={String(task.id)} task={task} onUpdate={refetch} />
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

function TaskRow({ task, onUpdate }: { task: Task; onUpdate: () => void }) {
  const archiveMutation = useMutation(archiveTask)

  const handleArchive = async () => {
    await archiveMutation.mutateAsync({ id: task.id })
    onUpdate()
  }

  return (
    <TableRow className={cn(isChildTask(task) && 'bg-muted/30')}>
      <TableCell>
        <Link
          to="/tasks/$id"
          params={{ id: String(task.id) }}
          className="text-primary hover:underline"
        >
          {task.name || `Unnamed - ${task.id}`}
        </Link>
      </TableCell>
      <TableCell>{task.workspace}</TableCell>
      <TableCell>
        <span className="flex items-center gap-2">
          <StatusBadge status={task.status} />
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
            Archive
          </Button>
        )}
      </TableCell>
    </TableRow>
  )
}

function StatusBadge({ status }: { status: string }) {
  const statusStyles: Record<string, string> = {
    starting: 'bg-amber-100 text-amber-800 border-amber-200',
    running: 'bg-blue-100 text-blue-800 border-blue-200',
    restarting: 'bg-pink-100 text-pink-800 border-pink-200',
    stopping: 'bg-orange-100 text-orange-800 border-orange-200',
    completed: 'bg-green-100 text-green-800 border-green-200',
    failed: 'bg-red-100 text-red-800 border-red-200',
    cancelled: 'bg-amber-100 text-amber-800 border-amber-200',
    archived: 'bg-gray-100 text-gray-600 border-gray-200',
  }

  return (
    <Badge
      variant="outline"
      className={statusStyles[status] ?? 'bg-gray-100 text-gray-600'}
    >
      {status}
    </Badge>
  )
}
