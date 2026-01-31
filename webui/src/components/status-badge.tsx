import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { Task } from '@/gen/xagent/v1/xagent_pb'

const statusStyles: Record<string, string> = {
  pending: 'bg-amber-100 text-amber-800 border-amber-200',
  running: 'bg-blue-100 text-blue-800 border-blue-200',
  restarting: 'bg-pink-100 text-pink-800 border-pink-200',
  cancelling: 'bg-orange-100 text-orange-800 border-orange-200',
  completed: 'bg-green-100 text-green-800 border-green-200',
  failed: 'bg-red-100 text-red-800 border-red-200',
  cancelled: 'bg-amber-100 text-amber-800 border-amber-200',
}

const activeStatuses = new Set(['running', 'restarting', 'cancelling'])

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
      {task.status}
    </Badge>
  )
}
