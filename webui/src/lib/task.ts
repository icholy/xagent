import type { Task } from '@/gen/xagent/v1/xagent_pb'

type TaskLike = Pick<Task, 'status' | 'actions'>
type TaskWithParent = Pick<Task, 'parent'>

export function isChildTask(task: TaskWithParent): boolean {
  return task.parent !== 0n
}

export function canArchiveTask(task: TaskLike): boolean {
  return task.actions?.archive ?? false
}

export function canCancelTask(task: TaskLike): boolean {
  return task.actions?.cancel ?? false
}

export function canRestartTask(task: TaskLike): boolean {
  return task.actions?.restart ?? false
}

export function isArchivedTask(task: TaskLike): boolean {
  return task.status === 'archived'
}
