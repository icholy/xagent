import { Outlet, createRootRouteWithContext, Link } from '@tanstack/react-router'
import { lazy, Suspense } from 'react'
import { QueryClient } from '@tanstack/react-query'
import xagentIcon from '@/assets/icon.png'

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
  return (
    <>
      <nav className="border-b">
        <div className="container mx-auto px-4 py-3 flex items-center gap-6">
          <Link to="/">
            <img src={xagentIcon} alt="XAgent" className="h-8 w-8" />
          </Link>
          <div className="flex gap-4">
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
              to="/tasks/new"
              className="text-muted-foreground hover:text-foreground transition-colors [&.active]:text-foreground"
            >
              Create Task
            </Link>
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
