import { timestampDate } from '@bufbuild/protobuf/wkt'
import type { Task } from '@/gen/xagent/v1/xagent_pb'
import { durationToMillis } from '@/lib/duration'

type TaskLike = Pick<Task, 'status' | 'actions' | 'archived'>
type TaskWithParent = Pick<Task, 'parent'>
type AutoArchiveTask = TaskLike & Pick<Task, 'archiveAfter' | 'updatedAt'>

export function isChildTask(task: TaskWithParent): boolean {
  return task.parent !== 0n
}

export function canArchiveTask(task: TaskLike): boolean {
  return task.actions?.archive ?? false
}

export function canUnarchiveTask(task: TaskLike): boolean {
  return task.actions?.unarchive ?? false
}

export function canCancelTask(task: TaskLike): boolean {
  return task.actions?.cancel ?? false
}

export function canRestartTask(task: TaskLike): boolean {
  return task.actions?.restart ?? false
}

export function isArchivedTask(task: TaskLike): boolean {
  return task.archived
}

// autoArchiveDeadline returns the time at which the task is scheduled to be
// auto-archived, or null when it isn't. The timer only runs once a task is in a
// terminal state, so this requires the task to be archivable (terminal and not
// yet archived), archive_after to be positive, and updated_at (the terminal
// timestamp) to be set.
export function autoArchiveDeadline(task: AutoArchiveTask): Date | null {
  if (!canArchiveTask(task) || !task.archiveAfter || !task.updatedAt) {
    return null
  }
  const afterMs = durationToMillis(task.archiveAfter)
  if (afterMs <= 0) {
    return null
  }
  return new Date(timestampDate(task.updatedAt).getTime() + afterMs)
}
