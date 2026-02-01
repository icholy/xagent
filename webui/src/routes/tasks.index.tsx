import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { useLocalStorage } from 'usehooks-ts'
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
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Input } from '@/components/ui/input'
import { RelativeTime } from '@/components/relative-time'
import { CommandBadge } from '@/components/command-badge'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'
import { Plus, Search, Loader2, X } from 'lucide-react'

export const Route = createFileRoute('/tasks/')({
  component: TasksPage,
})

function TasksPage() {
  const [showChildTasks, setShowChildTasks] = useLocalStorage('showChildTasks', false)
  const [searchQuery, setSearchQuery] = useState('')

  const { data, isLoading, error, refetch } = useQuery(listTasks, {}, {
    refetchInterval: 6000,
  })

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
    <div className="container mx-auto py-4 px-3 md:py-8 md:px-4">
      <div className="flex flex-col gap-3 mb-6 md:flex-row md:items-center md:justify-between">
        <h1 className="text-xl font-bold md:text-2xl">Tasks</h1>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:gap-4">
          <div className="flex items-center gap-2">
            <Label htmlFor="show-child-tasks" className="text-sm text-muted-foreground cursor-pointer">
              Show child tasks{hiddenCount > 0 && !showChildTasks && ` (${hiddenCount} hidden)`}
            </Label>
            <Switch
              id="show-child-tasks"
              checked={showChildTasks}
              onCheckedChange={setShowChildTasks}
            />
          </div>
          <div className="flex items-center gap-3">
            <div className="relative flex-1 sm:flex-initial">
              <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                type="text"
                placeholder="Search tasks..."
                className="pl-8 pr-8 w-full sm:w-48"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
              />
              {searchQuery && (
                <button
                  type="button"
                  onClick={() => setSearchQuery('')}
                  className="absolute right-2.5 top-2.5 h-4 w-4 text-muted-foreground hover:text-foreground"
                >
                  <X className="h-4 w-4" />
                </button>
              )}
            </div>
            <Link to="/tasks/new">
              <Button>
                <Plus className="h-4 w-4" />
                Task
              </Button>
            </Link>
          </div>
        </div>
      </div>
      {tasks.length === 0 ? (
        <div className="text-muted-foreground text-center py-8">
          No tasks found
        </div>
      ) : (
        <>
          {/* Mobile card view */}
          <div className="flex flex-col gap-3 md:hidden">
            {tasks.map((task) => (
              <TaskCard key={String(task.id)} task={task} onUpdate={refetch} />
            ))}
          </div>
          {/* Desktop table view */}
          <div className="hidden md:block">
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
                  <TaskRow key={String(task.id)} task={task} onUpdate={refetch} />
                ))}
              </TableBody>
            </Table>
          </div>
        </>
      )}
    </div>
  )
}

function TaskCard({ task, onUpdate }: { task: Task; onUpdate: () => void }) {
  const archiveMutation = useMutation(archiveTask, { onSuccess: () => onUpdate() })

  return (
    <div className={cn('rounded-lg border p-4 space-y-2', isChildTask(task) && 'bg-muted/30')}>
      <div className="flex items-start justify-between gap-2">
        <Link
          to="/tasks/$id"
          params={{ id: String(task.id) }}
          className="text-primary hover:underline font-medium break-words min-w-0"
        >
          {task.name || `Unnamed - ${task.id}`}
        </Link>
        {canArchiveTask(task) && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => archiveMutation.mutateAsync({ id: task.id })}
            disabled={archiveMutation.isPending}
            className="shrink-0"
          >
            {archiveMutation.isPending && <Loader2 className="mr-1 h-3 w-3 animate-spin" />}
            Archive
          </Button>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <StatusBadge task={task} />
        <CommandBadge task={task} />
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground">
        {task.workspace && <span>{task.workspace}</span>}
        {task.runner && <span>{task.runner}</span>}
        {task.createdAt && <RelativeTime date={timestampDate(task.createdAt)} />}
      </div>
    </div>
  )
}

function TaskRow({ task, onUpdate }: { task: Task; onUpdate: () => void }) {
  const archiveMutation = useMutation(archiveTask, { onSuccess: () => onUpdate() })

  const handleArchive = async () => {
    await archiveMutation.mutateAsync({ id: task.id })
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
