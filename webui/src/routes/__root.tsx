import { Outlet, createRootRouteWithContext, Link } from '@tanstack/react-router'
import { LogOut, Settings } from 'lucide-react'
import { lazy, Suspense, useState } from 'react'
import { QueryClient, useQueryClient } from '@tanstack/react-query'
import { useQuery } from '@connectrpc/connect-query'
import { getProfile } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import xagentIcon from '@/assets/icon.png'
import { authTransport } from '@/lib/transport'
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

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient
}>()({
  component: RootComponent,
})

function RootComponent() {
  const { data: profileData } = useQuery(getProfile, {})
  const queryClient = useQueryClient()

  const orgs = profileData?.orgs ?? []
  const [currentOrgId, setCurrentOrgId] = useState(() => authTransport.getOrgId())

  const handleOrgSwitch = async (orgId: string) => {
    await authTransport.fetchToken(orgId)
    setCurrentOrgId(authTransport.getOrgId())
    await queryClient.invalidateQueries()
  }

  return (
    <>
      <nav className="border-b">
        <div className="container mx-auto px-4 py-3 flex items-center gap-3 md:gap-6">
          <Link to="/tasks/new">
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
            )}
            {profileData?.profile?.email && (
              <span className="hidden md:inline text-sm text-muted-foreground">
                {profileData.profile.email}
              </span>
            )}
            <Link
              to="/settings"
              className="text-muted-foreground hover:text-foreground transition-colors [&.active]:text-foreground"
              title="Settings"
            >
              <Settings className="h-4 w-4" />
            </Link>
            <a
              href="/auth/logout"
              className="text-muted-foreground hover:text-foreground transition-colors text-sm flex items-center gap-1.5"
              title="Logout"
              onClick={() => authTransport.clearToken()}
            >
              <LogOut className="h-4 w-4" />
              <span className="hidden md:inline">Logout</span>
            </a>
          </div>
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
