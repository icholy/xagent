import { useSyncExternalStore } from 'react'
import { useAuthTransport } from '@/lib/services'

/** Returns the current org ID, re-rendering when it changes. */
export function useOrgId(): string {
  const auth = useAuthTransport()
  return useSyncExternalStore(
    (cb) => auth.onOrgChange(cb),
    () => auth.getOrgId(),
  )
}
