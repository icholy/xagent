import { useShellSessions } from '@/lib/services'
import { useShellState } from '@/hooks/use-shell-state'
import { TaskShell } from '@/components/task-shell'
import { Button } from '@/components/ui/button'
import { Loader2, Power, Terminal } from 'lucide-react'

interface TaskShellPanelProps {
  taskId: bigint
  // orgId is the active org, forwarded to the singleton for the cookie-
  // authenticated attach leg.
  orgId: string
  // canOpen mirrors canOpenShell(task): the sandbox can only be relaunched for a
  // finished task. When false and no session is live, the panel explains why.
  canOpen: boolean
}

// TaskShellPanel is the in-page Shell tab on the task detail page. The session
// and attach socket live in the ShellSessions singleton, so it survives tab
// switches (and unmount): this panel only reads phase and drives open()/close().
// Opening is behind an explicit click, so selecting the tab never relaunches the
// sandbox.
export function TaskShellPanel({ taskId, orgId, canOpen }: TaskShellPanelProps) {
  const shell = useShellSessions()
  const key = String(taskId)
  const { phase, error, started } = useShellState(key)
  const openSession = () => shell.open(key, orgId)

  return (
    <div className="flex h-full min-h-0 flex-col">
      {started && (
        <div className="flex items-center gap-2 border-b px-4 py-2 text-sm">
          <Terminal className="h-4 w-4 text-muted-foreground" />
          <span className="text-muted-foreground">Interactive shell</span>
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto text-destructive hover:text-destructive"
            onClick={() => shell.close(key)}
          >
            <Power className="mr-1 h-4 w-4" />
            Close shell
          </Button>
        </div>
      )}

      <div className="min-h-0 flex-1">
        {started ? (
          // A session has been minted; the terminal owns every state from here
          // (starting/connected/exited/error) and its own reconnect.
          <TaskShell taskId={taskId} orgId={orgId} />
        ) : phase === 'opening' ? (
          <Centered>
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            <p className="text-sm text-muted-foreground">Opening shell…</p>
          </Centered>
        ) : phase === 'error' ? (
          <Centered>
            <p className="text-sm text-destructive">Failed to open shell: {error}</p>
            <Button size="sm" variant="outline" onClick={openSession}>
              Try again
            </Button>
          </Centered>
        ) : canOpen ? (
          <Centered>
            <p className="text-sm text-muted-foreground">
              Attach an interactive shell to this task&apos;s sandbox.
            </p>
            <Button size="sm" onClick={openSession}>
              <Terminal className="mr-2 h-4 w-4" />
              Open shell
            </Button>
          </Centered>
        ) : (
          <Centered>
            <p className="max-w-md text-center text-sm text-muted-foreground">
              The shell attaches to a finished task&apos;s filesystem. Available once the task
              completes, fails, or is cancelled.
            </p>
          </Centered>
        )}
      </div>
    </div>
  )
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-4">{children}</div>
  )
}
