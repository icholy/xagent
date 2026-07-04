import { createFileRoute, Link } from '@tanstack/react-router'
import { useOrgId } from '@/hooks/use-org-id'
import { useShellSessions } from '@/lib/services'
import { useShellState } from '@/hooks/use-shell-state'
import { TaskShell } from '@/components/task-shell'
import { Button } from '@/components/ui/button'
import { ArrowLeft, Loader2, TerminalSquare } from 'lucide-react'

export const Route = createFileRoute('/tasks/$id_/shell')({
  staticData: { orgSwitchRedirect: '/tasks' },
  component: TaskShellPage,
})

function TaskShellPage() {
  const { id } = Route.useParams()
  const taskId = BigInt(id)
  const orgId = useOrgId()
  const shell = useShellSessions()
  const key = String(taskId)

  // The session + attach socket live in the ShellSessions singleton; the page
  // just reads phase and drives open(). Opening is behind an explicit click (no
  // auto-connect on mount), so navigating here never relaunches the sandbox.
  const { phase, error, started } = useShellState(key)
  const openSession = () => shell.open(key, orgId)

  return (
    <div className="flex h-screen flex-col">
      <div className="flex items-center gap-3 border-b px-4 py-2">
        <Button asChild variant="ghost" size="sm">
          <Link to="/tasks/$id" params={{ id }} search={{ org: orgId }}>
            <ArrowLeft className="mr-1 h-4 w-4" />
            Back to task
          </Link>
        </Button>
        <div className="flex items-center gap-2 text-sm font-medium">
          <TerminalSquare className="h-4 w-4" />
          Shell · task {id}
        </div>
      </div>

      <div className="min-h-0 flex-1">
        {started ? (
          // A session has been minted; the terminal owns every state from here
          // (starting/connected/exited/error) and its own reconnect.
          <TaskShell taskId={taskId} orgId={orgId} />
        ) : phase === 'opening' ? (
          <div className="flex h-full flex-col items-center justify-center gap-3">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            <p className="text-sm text-muted-foreground">Opening shell…</p>
          </div>
        ) : phase === 'error' ? (
          <div className="flex h-full flex-col items-center justify-center gap-3">
            <p className="text-sm text-destructive">Failed to open shell: {error}</p>
            <Button size="sm" variant="outline" onClick={openSession}>
              Try again
            </Button>
          </div>
        ) : (
          <div className="flex h-full flex-col items-center justify-center gap-3">
            <p className="text-sm text-muted-foreground">
              Attach an interactive shell to this task&apos;s sandbox.
            </p>
            <Button size="sm" onClick={openSession}>
              <TerminalSquare className="mr-2 h-4 w-4" />
              Open shell
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
