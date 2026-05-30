import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/events/')({
  beforeLoad: () => {
    throw redirect({ to: '/settings', search: { tab: 'events' }, replace: true })
  },
})
