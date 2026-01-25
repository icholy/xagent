import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import { listWorkspaces, clearWorkspaces } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Button } from '@/components/ui/button'
import { RelativeTime } from '@/components/relative-time'
import { Trash2 } from 'lucide-react'

export const Route = createFileRoute('/workspaces/')({
  component: WorkspacesPage,
})

function WorkspacesPage() {
  const [selectedRunner, setSelectedRunner] = useState<string>('all')
  const { data, isLoading, error, refetch } = useQuery(listWorkspaces, {}, {
    refetchInterval: 5000,
  })
  const clearMutation = useMutation(clearWorkspaces)

  const handleClear = async () => {
    await clearMutation.mutateAsync({})
    refetch()
  }

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

  const allWorkspaces = data?.workspaces ?? []
  const runners = [...new Set(allWorkspaces.map((w) => w.runnerId).filter(Boolean))]
  const workspaces = selectedRunner === 'all'
    ? allWorkspaces
    : allWorkspaces.filter((w) => w.runnerId === selectedRunner)

  return (
    <div className="container mx-auto py-8 px-4">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Workspaces</h1>
        <div className="flex items-center gap-4">
          <Select value={selectedRunner} onValueChange={setSelectedRunner}>
            <SelectTrigger className="w-48">
              <SelectValue placeholder="Filter by runner" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All runners</SelectItem>
              {runners.map((runner) => (
                <SelectItem key={runner} value={runner!}>
                  {runner}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            onClick={handleClear}
            disabled={clearMutation.isPending || allWorkspaces.length === 0}
          >
            <Trash2 className="h-4 w-4" />
            Clear
          </Button>
        </div>
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
