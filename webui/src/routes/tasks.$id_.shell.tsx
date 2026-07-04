import { createFileRoute, Link } from '@tanstack/react-router'
import { useMutation } from '@connectrpc/connect-query'
import { openShell } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { useEffect, useState } from 'react'
import { useOrgId } from '@/hooks/use-org-id'
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
  const [session, setSession] = useState<string | null>(null)

  // OpenShell relaunches the finished task's sandbox as a reverse shell and
  // returns the rendezvous id. Reconnecting mints a fresh session (the relay has
  // no scrollback replay), so the same mutation drives both the initial open and
  // every reconnect. The session id is set from the mutation's onSuccess callback
  // rather than an awaited result so we never call setState inside the effect body.
  const open = useMutation(openShell, {
    onSuccess: (resp) => setSession(resp.sessionId),
  })
  const openSession = () => open.mutate({ taskId })

  useEffect(() => {
    openSession()
    // Open exactly once on mount; reconnects go through the button.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

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
        {session ? (
          <TaskShell
            session={session}
            orgId={orgId}
            onReconnect={openSession}
            reconnecting={open.isPending}
          />
        ) : (
          <div className="flex h-full flex-col items-center justify-center gap-3">
            {open.error ? (
              <>
                <p className="text-sm text-destructive">
                  Failed to open shell: {open.error.message}
                </p>
                <Button size="sm" variant="outline" onClick={openSession}>
                  Try again
                </Button>
              </>
            ) : (
              <>
                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                <p className="text-sm text-muted-foreground">Opening shell…</p>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
