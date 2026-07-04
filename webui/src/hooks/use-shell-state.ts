import { useCallback, useSyncExternalStore } from 'react'
import { useShellSessions } from '@/lib/services'
import type { ShellState } from '@/lib/shell-sessions'

/** Subscribes to a task's shell state, re-rendering when it changes. */
export function useShellState(taskKey: string): ShellState {
  const shell = useShellSessions()
  const subscribe = useCallback((cb: () => void) => shell.subscribe(taskKey, cb), [shell, taskKey])
  const getSnapshot = useCallback(() => shell.getSnapshot(taskKey), [shell, taskKey])
  return useSyncExternalStore(subscribe, getSnapshot)
}
