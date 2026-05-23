import { createFileRoute, redirect } from '@tanstack/react-router'
import { NO_ORG } from '@/lib/transport'

export const Route = createFileRoute('/')({
  beforeLoad: ({ context: { auth } }) => {
    const orgId = auth.getOrgId()
    throw redirect({
      to: '/tasks',
      search: orgId === NO_ORG ? {} : { org: orgId },
    })
  },
})
