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
    <div className="container mx-auto py-8 px-4">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Tasks</h1>
        <div className="flex items-center gap-4">
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              type="text"
              placeholder="Search tasks..."
              className="pl-8 pr-8 w-48"
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
      )}
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

