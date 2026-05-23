import { Outlet, createRootRouteWithContext, Link, useNavigate, useMatches } from '@tanstack/react-router'
import { LogOut, Settings } from 'lucide-react'
import { lazy, Suspense, useEffect } from 'react'
import { QueryClient } from '@tanstack/react-query'
import { useQuery } from '@connectrpc/connect-query'
import { getProfile } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import xagentIcon from '@/assets/icon.png'
import { useAuthTransport } from '@/lib/services'
import { NO_ORG, type AuthTransport } from '@/lib/transport'
import { useOrgId } from '@/hooks/use-org-id'
import { useOrgSSE } from '@/hooks/use-org-sse'
import { ConnectionIndicator } from '@/components/connection-indicator'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

// TanStack devtools check NODE_ENV and render nothing in production, but the
// devtools code is still bundled. Use lazy loading with import.meta.env.DEV to
// completely exclude devtools from production builds.
// https://github.com/TanStack/router/issues/1383
const TanStackRouterDevtools = import.meta.env.DEV
  ? lazy(() =>
      import('@tanstack/router-devtools').then((res) => ({
        default: res.TanStackRouterDevtools,
      }))
    )
  : () => null

const ReactQueryDevtools = import.meta.env.DEV
  ? lazy(() =>
      import('@tanstack/react-query-devtools').then((res) => ({
        default: res.ReactQueryDevtools,
      }))
    )
  : () => null

type RootSearch = { org?: string }

function validateSearch(search: Record<string, unknown>): RootSearch {
  return { org: typeof search.org === 'string' ? search.org : undefined }
}

async function beforeLoad({
  search,
  context: { auth, queryClient },
}: {
  search: RootSearch
  context: { auth: AuthTransport; queryClient: QueryClient }
}): Promise<void> {
  if (search.org && search.org !== auth.getOrgId()) {
    await auth.fetchToken(search.org)
    queryClient.removeQueries()
  }
}

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient
  auth: AuthTransport
}>()({
  validateSearch,
  beforeLoad,
  component: RootComponent,
})

function RootComponent() {
  useOrgSSE()
  const { data: profileData } = useQuery(getProfile, {})
  const auth = useAuthTransport()

  const orgs = profileData?.orgs ?? []
  const currentOrgId = useOrgId()
  const searchOrg = Route.useSearch({ select: (s) => s.org })

  const navigate = useNavigate()
  const route = useMatches().at(-1)

  // Keep ?org= in the URL in sync with the active org. Covers cold loads
  // and silent recoveries (e.g. fetchToken falling back from a stale org).
  useEffect(() => {
    if (currentOrgId === NO_ORG) return
    if (searchOrg === currentOrgId) return
    navigate({
      to: '.',
      search: (prev) => ({ ...prev, org: currentOrgId }),
      replace: true,
    })
  }, [currentOrgId, searchOrg, navigate])

  const handleOrgSwitch = async (orgId: string) => {
    const redirect = route?.staticData.orgSwitchRedirect
    if (redirect) {
      await navigate({ to: redirect, search: { org: orgId } })
    } else {
      await navigate({
        to: '.',
        search: (prev) => ({ ...prev, org: orgId }),
        replace: true,
      })
    }
  }

  return (
    <>
      <nav className="border-b">
        <div className="container mx-auto px-4 py-3 flex flex-wrap items-center gap-3 md:gap-6">
          <Link to="/tasks/new" className="hidden md:block">
            <img src={xagentIcon} alt="XAgent" className="h-8 w-8" />
          </Link>
          <div className="flex gap-2 md:gap-4">
            <Link
              to="/tasks"
              className="text-muted-foreground hover:text-foreground transition-colors [&.active]:text-foreground"
            >
              Tasks
            </Link>
            <Link
              to="/events"
              className="text-muted-foreground hover:text-foreground transition-colors [&.active]:text-foreground"
            >
              Events
            </Link>
            <Link
              to="/workspaces"
              className="text-muted-foreground hover:text-foreground transition-colors [&.active]:text-foreground"
            >
              Workspaces
            </Link>
            <Link
              to="/members"
              className="text-muted-foreground hover:text-foreground transition-colors [&.active]:text-foreground"
            >
              Members
            </Link>
            <Link
              to="/keys"
              className="text-muted-foreground hover:text-foreground transition-colors [&.active]:text-foreground"
            >
              Keys
            </Link>
          </div>
          <div className="ml-auto flex items-center gap-4">
            {orgs.length > 0 && (
              <div className="hidden md:block">
                <Select value={currentOrgId} onValueChange={handleOrgSwitch}>
                  <SelectTrigger className="w-40 h-8 text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {orgs.map((org) => (
                      <SelectItem key={String(org.id)} value={String(org.id)}>
                        {org.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
            <span className="hidden md:inline-flex">
              <ConnectionIndicator />
            </span>
            <Link
              to="/settings"
              search={{ tab: 'account' }}
              activeOptions={{ includeSearch: false }}
              className="text-muted-foreground hover:text-foreground transition-colors [&.active]:text-foreground"
              title="Settings"
            >
              <Settings className="h-4 w-4" />
            </Link>
            <a
              href="/auth/logout"
              className="text-muted-foreground hover:text-foreground transition-colors text-sm flex items-center gap-1.5"
              title="Logout"
              onClick={() => auth.clearToken()}
            >
              <LogOut className="h-4 w-4" />
              <span className="hidden md:inline">Logout</span>
            </a>
          </div>
          {orgs.length > 0 && (
            <div className="basis-full md:hidden flex items-center gap-3">
              <Select value={currentOrgId} onValueChange={handleOrgSwitch}>
                <SelectTrigger className="flex-1 h-8 text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {orgs.map((org) => (
                    <SelectItem key={String(org.id)} value={String(org.id)}>
                      {org.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <ConnectionIndicator />
            </div>
          )}
        </div>
      </nav>
      <Outlet />
      <Suspense>
        <ReactQueryDevtools buttonPosition="top-right" />
      </Suspense>
      <Suspense>
        <TanStackRouterDevtools position="bottom-right" />
      </Suspense>
    </>
  )
}
