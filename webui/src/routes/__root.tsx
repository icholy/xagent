import { Outlet, createRootRouteWithContext, Link } from '@tanstack/react-router'
import { LogOut, Menu } from 'lucide-react'
import { lazy, Suspense, useState } from 'react'
import { QueryClient } from '@tanstack/react-query'
import { useQuery } from '@connectrpc/connect-query'
import { getProfile } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetTrigger,
  SheetContent,
  SheetTitle,
} from '@/components/ui/sheet'
import xagentIcon from '@/assets/icon.png'

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

const navLinks = [
  { to: '/tasks' as const, label: 'Tasks' },
  { to: '/events' as const, label: 'Events' },
  { to: '/workspaces' as const, label: 'Workspaces' },
  { to: '/keys' as const, label: 'API Keys' },
]

function RootComponent() {
  const { data: profileData } = useQuery(getProfile, {})
  const [mobileOpen, setMobileOpen] = useState(false)

  return (
    <>
      <nav className="border-b">
        <div className="container mx-auto px-4 py-3 flex items-center gap-6">
          <Link to="/">
            <img src={xagentIcon} alt="XAgent" className="h-8 w-8" />
          </Link>
          {/* Desktop nav */}
          <div className="hidden md:flex gap-4">
            {navLinks.map((link) => (
              <Link
                key={link.to}
                to={link.to}
                className="text-muted-foreground hover:text-foreground transition-colors [&.active]:text-foreground"
              >
                {link.label}
              </Link>
            ))}
          </div>
          <div className="ml-auto hidden md:flex items-center gap-4">
            {profileData?.profile?.email && (
              <span className="text-sm text-muted-foreground">
                {profileData.profile.email}
              </span>
            )}
            <a
              href="/auth/logout"
              className="text-muted-foreground hover:text-foreground transition-colors text-sm flex items-center gap-1.5"
            >
              <LogOut className="h-4 w-4" />
              Logout
            </a>
          </div>
          {/* Mobile hamburger */}
          <div className="ml-auto md:hidden">
            <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
              <SheetTrigger asChild>
                <Button variant="ghost" size="icon">
                  <Menu className="h-5 w-5" />
                  <span className="sr-only">Open menu</span>
                </Button>
              </SheetTrigger>
              <SheetContent>
                <SheetTitle>Navigation</SheetTitle>
                <div className="flex flex-col gap-4 mt-4">
                  {navLinks.map((link) => (
                    <Link
                      key={link.to}
                      to={link.to}
                      className="text-foreground hover:text-primary transition-colors text-lg [&.active]:text-primary"
                      onClick={() => setMobileOpen(false)}
                    >
                      {link.label}
                    </Link>
                  ))}
                </div>
                <div className="mt-auto pt-6 border-t flex flex-col gap-3">
                  {profileData?.profile?.email && (
                    <span className="text-sm text-muted-foreground truncate">
                      {profileData.profile.email}
                    </span>
                  )}
                  <a
                    href="/auth/logout"
                    className="text-muted-foreground hover:text-foreground transition-colors text-sm flex items-center gap-1.5"
                  >
                    <LogOut className="h-4 w-4" />
                    Logout
                  </a>
                </div>
              </SheetContent>
            </Sheet>
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
