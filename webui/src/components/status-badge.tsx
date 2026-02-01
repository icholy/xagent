import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { TaskStatus, type Task } from '@/gen/xagent/v1/xagent_pb'

const statusStyles: Record<TaskStatus, string> = {
  [TaskStatus.UNSPECIFIED]: 'bg-gray-100 text-gray-600 border-gray-200',
  [TaskStatus.PENDING]: 'bg-amber-100 text-amber-800 border-amber-200',
  [TaskStatus.RUNNING]: 'bg-blue-100 text-blue-800 border-blue-200',
  [TaskStatus.RESTARTING]: 'bg-pink-100 text-pink-800 border-pink-200',
  [TaskStatus.CANCELLING]: 'bg-orange-100 text-orange-800 border-orange-200',
  [TaskStatus.COMPLETED]: 'bg-green-100 text-green-800 border-green-200',
  [TaskStatus.FAILED]: 'bg-red-100 text-red-800 border-red-200',
  [TaskStatus.CANCELLED]: 'bg-amber-100 text-amber-800 border-amber-200',
}

const statusLabels: Record<TaskStatus, string> = {
  [TaskStatus.UNSPECIFIED]: 'unknown',
  [TaskStatus.PENDING]: 'pending',
  [TaskStatus.RUNNING]: 'running',
  [TaskStatus.RESTARTING]: 'restarting',
  [TaskStatus.CANCELLING]: 'cancelling',
  [TaskStatus.COMPLETED]: 'completed',
  [TaskStatus.FAILED]: 'failed',
  [TaskStatus.CANCELLED]: 'cancelled',
}

const activeStatuses = new Set([TaskStatus.RUNNING, TaskStatus.RESTARTING, TaskStatus.CANCELLING])

export function StatusBadge({ task }: { task: Task }) {
  const isActive = activeStatuses.has(task.status)

  return (
    <Badge
      variant="outline"
      className={cn(statusStyles[task.status] ?? 'bg-gray-100 text-gray-600')}
    >
      {isActive && (
        <span className="relative flex h-2 w-2 mr-1">
          <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-current opacity-75"></span>
          <span className="relative inline-flex rounded-full h-2 w-2 bg-current"></span>
        </span>
      )}
      {statusLabels[task.status] ?? 'unknown'}
    </Badge>
  )
}
