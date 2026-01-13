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
import { StatusBadge } from '@/components/ui/status-badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Input } from '@/components/ui/input'
import { RelativeTime } from '@/components/ui/relative-time'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'
import { Plus, Search } from 'lucide-react'

export const Route = createFileRoute('/tasks/')({
  component: TasksPage,
})

function TasksPage() {
  const [showChildTasks, setShowChildTasks] = useState(() => {
    const stored = localStorage.getItem('showChildTasks')
    return stored !== null ? stored === 'true' : false
  })
  const [searchQuery, setSearchQuery] = useState('')

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
  const search = searchQuery.trim().toLowerCase()
  const tasks = allTasks.filter((task) => {
    if (!showChildTasks && isChildTask(task)) {
      return false
    }
    if (search && !(task.name || `Unnamed - ${task.id}`).toLowerCase().includes(search)) {
      return false
    }
    return true
  })
  const hiddenCount = allTasks.filter(isChildTask).length

  return (
    <div className="container mx-auto py-8 px-4">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Tasks</h1>
        <div className="flex items-center gap-4">
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              type="search"
              placeholder="Search tasks..."
              className="pl-8 w-48"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
            />
          </div>
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
          {task.command && <CommandBadge command={task.command} />}
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

const commandStyles: Record<string, string> = {
  restart: 'bg-pink-100 text-pink-800 border-pink-200',
  stop: 'bg-orange-100 text-orange-800 border-orange-200',
}

function CommandBadge({ command }: { command: string }) {
  return (
    <Badge
      variant="outline"
      className={commandStyles[command] ?? 'bg-gray-100 text-gray-600'}
    >
      command:{command}
    </Badge>
  )
}
