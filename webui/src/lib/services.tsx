import { createContext, useContext, type ReactNode } from 'react'
import type { AuthTransport } from './transport'
import type { NotificationSSE } from './notification-sse'
import type { ShellSessions } from './shell-sessions'

export interface Services {
  auth: AuthTransport
  notifications: NotificationSSE
  shell: ShellSessions
}

const ServicesContext = createContext<Services | null>(null)

export function ServicesProvider({
  services,
  children,
}: {
  services: Services
  children: ReactNode
}) {
  return <ServicesContext.Provider value={services}>{children}</ServicesContext.Provider>
}

export function useAuthTransport(): AuthTransport {
  const s = useContext(ServicesContext)
  if (!s) {
    throw new Error('useAuthTransport must be used within ServicesProvider')
  }
  return s.auth
}

export function useNotificationSSE(): NotificationSSE {
  const s = useContext(ServicesContext)
  if (!s) {
    throw new Error('useNotificationSSE must be used within ServicesProvider')
  }
  return s.notifications
}

export function useShellSessions(): ShellSessions {
  const s = useContext(ServicesContext)
  if (!s) {
    throw new Error('useShellSessions must be used within ServicesProvider')
  }
  return s.shell
}
