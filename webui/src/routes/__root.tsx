import { Outlet, createRootRouteWithContext, Link, useRouterState } from '@tanstack/react-router'
import { TanStackRouterDevtools } from '@tanstack/router-devtools'
import { QueryClient } from '@tanstack/react-query'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import { Button } from '@/components/ui/button'
import { Plus } from 'lucide-react'

export const Route = createRootRouteWithContext<{
  queryClient: QueryClient
}>()({
  component: RootComponent,
})

function RootComponent() {
  const routerState = useRouterState()
  const pathname = routerState.location.pathname

  // Show "+ Task" button on /tasks, "+ Event" button on /events
  const showTaskButton = pathname === '/tasks' || pathname === '/tasks/'
  const showEventButton = pathname === '/events' || pathname === '/events/'

  return (
    <>
      <nav className="border-b">
        <div className="container mx-auto px-4 py-3 flex items-center gap-6">
          <Link to="/" className="font-semibold text-lg">
            XAgent
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
          </div>
          <div className="flex-1" />
          {showTaskButton && (
            <Link to="/tasks/new">
              <Button>
                <Plus className="h-4 w-4" />
                Task
              </Button>
            </Link>
          )}
          {showEventButton && (
            <Link to="/events/new">
              <Button>
                <Plus className="h-4 w-4" />
                Event
              </Button>
            </Link>
          )}
        </div>
      </nav>
      <Outlet />
      <ReactQueryDevtools buttonPosition="top-right" />
      <TanStackRouterDevtools position="bottom-right" />
    </>
  )
}
