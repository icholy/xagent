import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { keepPreviousData } from '@tanstack/react-query'
import { listTasks, archiveTask } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Task } from '@/gen/xagent/v1/xagent_pb'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { canArchiveTask } from '@/lib/task'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { StatusBadge } from '@/components/status-badge'
import { ArchiveButton } from '@/components/archive-button'
import { Button } from '@/components/ui/button'
import { RelativeTime } from '@/components/relative-time'
import { CommandBadge } from '@/components/command-badge'
import { Plus } from 'lucide-react'
import { useOrgId } from '@/hooks/use-org-id'

export const Route = createFileRoute('/tasks/')({
  component: TasksPage,
})

function TasksPage() {
  const orgId = useOrgId()

  // Stack of page tokens for the pages navigated past the first. The current
  // page's token is the top of the stack; an empty stack is the first page.
  const [tokens, setTokens] = useState<string[]>([])
  const pageToken = tokens.at(-1) ?? ''

  const { data, isLoading, error, isPlaceholderData, refetch } = useQuery(
    listTasks,
    { pageSize: 50, pageToken },
    {
      placeholderData: keepPreviousData,
      refetchInterval: 60000,
    },
  )

  const nextPageToken = data?.nextPageToken ?? ''
  const goNext = () => {
    if (nextPageToken) setTokens((t) => [...t, nextPageToken])
  }
  const goPrev = () => setTokens((t) => t.slice(0, -1))

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

  const tasks = data?.tasks ?? []

  return (
    <div className="container mx-auto py-8 px-4">
      <div className="flex flex-wrap items-center justify-between gap-4 mb-6">
        <h1 className="text-2xl font-bold">Tasks</h1>
        <div className="flex flex-wrap items-center gap-4">
          <Link to="/tasks/new" search={{ org: orgId }}>
            <Button>
              <Plus className="h-4 w-4" />
              Task
            </Button>
          </Link>
        </div>
      </div>
      {tasks.length === 0 ? (
        <div className="text-muted-foreground text-center py-8">No tasks found</div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead className="hidden md:table-cell">Runner</TableHead>
              <TableHead className="hidden md:table-cell">Workspace</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="hidden md:table-cell">Created</TableHead>
              <TableHead className="hidden md:table-cell"></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tasks.map((task) => (
              <TaskRow key={String(task.id)} task={task} onUpdate={refetch} />
            ))}
          </TableBody>
        </Table>
      )}
      {(tokens.length > 0 || nextPageToken) && (
        <div className="flex justify-center gap-2 py-6">
          <Button variant="outline" onClick={goPrev} disabled={tokens.length === 0}>
            Previous
          </Button>
          <Button variant="outline" onClick={goNext} disabled={!nextPageToken || isPlaceholderData}>
            Next
          </Button>
        </div>
      )}
    </div>
  )
}

function TaskRow({ task, onUpdate }: { task: Task; onUpdate: () => void }) {
  const orgId = useOrgId()
  const archiveMutation = useMutation(archiveTask, { onSuccess: () => onUpdate() })

  const handleArchive = async () => {
    await archiveMutation.mutateAsync({ id: task.id })
  }

  return (
    <TableRow>
      <TableCell>
        <Link
          to="/tasks/$id"
          search={{ org: orgId }}
          params={{ id: String(task.id) }}
          className="text-primary hover:underline"
        >
          {task.name || `Unnamed - ${task.id}`}
        </Link>
      </TableCell>
      <TableCell className="hidden md:table-cell">{task.runner}</TableCell>
      <TableCell className="hidden md:table-cell">{task.workspace}</TableCell>
      <TableCell>
        <span className="flex items-center gap-2">
          <StatusBadge task={task} />
          <CommandBadge task={task} />
        </span>
      </TableCell>
      <TableCell className="hidden md:table-cell text-muted-foreground">
        {task.createdAt ? <RelativeTime date={timestampDate(task.createdAt)} /> : '-'}
      </TableCell>
      <TableCell className="hidden md:table-cell">
        {canArchiveTask(task) && (
          <ArchiveButton
            task={task}
            onArchive={handleArchive}
            pending={archiveMutation.isPending}
            compact
          />
        )}
      </TableCell>
    </TableRow>
  )
}
