import { useSyncExternalStore } from 'react'
import { authTransport } from '@/lib/transport'

/** Returns the current org ID, re-rendering when it changes. */
export function useOrgId(): string {
  return useSyncExternalStore(
    (cb) => authTransport.subscribe(cb),
    () => authTransport.getOrgId(),
  )
}
