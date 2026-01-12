import { Outlet, createRootRouteWithContext, Link } from '@tanstack/react-router'
import { TanStackRouterDevtools } from '@tanstack/router-devtools'
import { QueryClient } from '@tanstack/react-query'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import xagentIcon from '@/assets/icon.png'

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
      <ReactQueryDevtools buttonPosition="top-right" />
      <TanStackRouterDevtools position="bottom-right" />
    </>
  )
}
