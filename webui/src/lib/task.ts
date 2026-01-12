import type { Task } from '@/gen/xagent/v1/xagent_pb'

type TaskLike = Pick<Task, 'status'>
type TaskWithParent = Pick<Task, 'parent'>

export function isChildTask(task: TaskWithParent): boolean {
  return task.parent !== 0n
}

export function canArchiveTask(task: TaskLike): boolean {
  return task.status === 'completed' || task.status === 'failed'
}

export function canCancelTask(task: TaskLike): boolean {
  return task.status === 'running' || task.status === 'pending'
}

export function canRestartTask(task: TaskLike): boolean {
  return task.status === 'running' || task.status === 'completed' || task.status === 'failed'
}

export function isArchivedTask(task: TaskLike): boolean {
  return task.status === 'archived'
}
