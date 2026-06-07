import { useEffect, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { autoArchiveDeadline } from '@/lib/task'
import { formatCountdown } from '@/lib/duration'
import type { Task } from '@/gen/xagent/v1/xagent_pb'

type ArchiveTask = Pick<Task, 'status' | 'actions' | 'archived' | 'autoArchive' | 'updatedAt'>

// useAutoArchiveCountdown returns a live-ticking, human-readable string of the
// time until the task is auto-archived (e.g. "5m"), or null when the task isn't
// scheduled for auto-archiving.
function useAutoArchiveCountdown(task: ArchiveTask): string | null {
  const deadlineTime = autoArchiveDeadline(task)?.getTime() ?? null
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (deadlineTime === null) return
    const interval = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(interval)
  }, [deadlineTime])
  if (deadlineTime === null) return null
  return formatCountdown(deadlineTime - now)
}

// ArchiveButton renders the manual archive control. When the task is scheduled
// to be auto-archived, it shows a live countdown in the label ("Archive (5m)")
// so the automatic behavior is co-located with the manual action.
export function ArchiveButton({
  task,
  onArchive,
  pending,
  disabled,
}: {
  task: ArchiveTask
  onArchive: () => void
  pending: boolean
  disabled?: boolean
}) {
  const countdown = useAutoArchiveCountdown(task)
  return (
    <Button
      variant="outline"
      size="sm"
      onClick={onArchive}
      disabled={disabled ?? pending}
      title={countdown ? `Auto-archives in ${countdown}` : undefined}
    >
      {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
      {countdown ? `Archive (${countdown})` : 'Archive'}
    </Button>
  )
}
