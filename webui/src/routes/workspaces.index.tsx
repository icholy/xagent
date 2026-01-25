import { createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@connectrpc/connect-query'
import { listWorkspaces } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { RelativeTime } from '@/components/relative-time'

export const Route = createFileRoute('/workspaces/')({
  component: WorkspacesPage,
})

function WorkspacesPage() {
  const { data, isLoading, error } = useQuery(listWorkspaces, {}, {
    refetchInterval: 5000,
  })

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Loading workspaces...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-destructive">Error: {error.message}</div>
      </div>
    )
  }

  const workspaces = data?.workspaces ?? []

  return (
    <div className="container mx-auto py-8 px-4">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Workspaces</h1>
      </div>
      {workspaces.length === 0 ? (
        <div className="text-muted-foreground text-center py-8">
          No workspaces registered
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Runner</TableHead>
              <TableHead>Last Updated</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {workspaces.map((workspace) => (
              <TableRow key={workspace.name}>
                <TableCell className="font-medium">{workspace.name}</TableCell>
                <TableCell className="text-muted-foreground font-mono text-sm">
                  {workspace.runnerId || '-'}
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {workspace.updatedAt ? (
                    <RelativeTime date={timestampDate(workspace.updatedAt)} />
                  ) : (
                    '-'
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}
