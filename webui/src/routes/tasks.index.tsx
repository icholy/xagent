import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery } from '@connectrpc/connect-query'
import { listTasks } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Task } from '@/gen/xagent/v1/xagent_pb'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/tasks/')({
  component: TasksPage,
})

function TasksPage() {
  const [showChildTasks, setShowChildTasks] = useState(() => {
    const stored = localStorage.getItem('showChildTasks')
    return stored !== null ? stored === 'true' : true
  })

  const { data, isLoading, error } = useQuery(listTasks, {}, {
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
        <div className="flex items-center gap-2">
          <Switch
            id="show-child-tasks"
            checked={showChildTasks}
            onCheckedChange={handleToggleChildTasks}
          />
          <Label htmlFor="show-child-tasks" className="text-sm text-muted-foreground cursor-pointer">
            Show child tasks{hiddenCount > 0 && !showChildTasks && ` (${hiddenCount} hidden)`}
          </Label>
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
            </TableRow>
          </TableHeader>
          <TableBody>
            {tasks.map((task) => (
              <TaskRow key={String(task.id)} task={task} />
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

function TaskRow({ task }: { task: Task }) {
  const isChild = task.parent !== 0n

  return (
    <TableRow className={cn(isChild && 'bg-muted/30')}>
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
        {task.createdAt ? formatDate(timestampDate(task.createdAt)) : '-'}
      </TableCell>
    </TableRow>
  )
}

function StatusBadge({ status }: { status: string }) {
  const statusStyles: Record<string, string> = {
    pending: 'bg-amber-100 text-amber-800 border-amber-200',
    running: 'bg-blue-100 text-blue-800 border-blue-200',
    completed: 'bg-green-100 text-green-800 border-green-200',
    failed: 'bg-red-100 text-red-800 border-red-200',
    cancelled: 'bg-amber-100 text-amber-800 border-amber-200',
    restarting: 'bg-pink-100 text-pink-800 border-pink-200',
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

function formatDate(date: Date): string {
  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}
