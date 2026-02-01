import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  listKeys,
  deleteKey,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Key } from '@/gen/xagent/v1/xagent_pb'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { RelativeTime } from '@/components/relative-time'
import { Plus, Trash2, Loader2 } from 'lucide-react'

export const Route = createFileRoute('/keys/')({
  component: KeysPage,
})

function KeysPage() {
  const { data, isLoading, error, refetch } = useQuery(listKeys, {}, {
    refetchInterval: 6000,
  })

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Loading API keys...</div>
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

  const keys = data?.keys ?? []

  return (
    <div className="container mx-auto py-4 px-3 md:py-8 md:px-4">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-xl font-bold md:text-2xl">API Keys</h1>
        <Link to="/keys/new">
          <Button>
            <Plus className="h-4 w-4" />
            API Key
          </Button>
        </Link>
      </div>
      {keys.length === 0 ? (
        <div className="text-muted-foreground text-center py-8">
          No API keys found
        </div>
      ) : (
        <>
          {/* Mobile card view */}
          <div className="flex flex-col gap-3 md:hidden">
            {keys.map((key) => (
              <KeyCard key={key.id} apiKey={key} onDelete={refetch} />
            ))}
          </div>
          {/* Desktop table view */}
          <div className="hidden md:block">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.map((key) => (
                  <KeyRow
                    key={key.id}
                    apiKey={key}
                    onDelete={refetch}
                  />
                ))}
              </TableBody>
            </Table>
          </div>
        </>
      )}
    </div>
  )
}

function KeyCard({
  apiKey,
  onDelete,
}: {
  apiKey: Key
  onDelete: () => void
}) {
  const deleteMutation = useMutation(deleteKey, {
    onSuccess: () => onDelete(),
  })

  const isExpired = apiKey.expiresAt && timestampDate(apiKey.expiresAt) < new Date()

  return (
    <div className="rounded-lg border p-4 space-y-2">
      <div className="flex items-start justify-between gap-2">
        <span className="font-medium">{apiKey.name}</span>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => deleteMutation.mutateAsync({ id: apiKey.id })}
          disabled={deleteMutation.isPending}
          className="shrink-0"
        >
          {deleteMutation.isPending ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Trash2 className="h-4 w-4" />
          )}
          Delete
        </Button>
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground">
        <span>
          Expires:{' '}
          {apiKey.expiresAt ? (
            <span className={isExpired ? 'text-destructive' : ''}>
              {isExpired ? 'Expired ' : ''}
              <RelativeTime date={timestampDate(apiKey.expiresAt)} />
            </span>
          ) : (
            'Never'
          )}
        </span>
        {apiKey.createdAt && (
          <span>
            Created: <RelativeTime date={timestampDate(apiKey.createdAt)} />
          </span>
        )}
      </div>
    </div>
  )
}

function KeyRow({
  apiKey,
  onDelete,
}: {
  apiKey: Key
  onDelete: () => void
}) {
  const deleteMutation = useMutation(deleteKey, {
    onSuccess: () => onDelete(),
  })

  const handleDelete = async () => {
    await deleteMutation.mutateAsync({ id: apiKey.id })
  }

  const isExpired = apiKey.expiresAt && timestampDate(apiKey.expiresAt) < new Date()

  return (
    <TableRow>
      <TableCell className="font-medium">{apiKey.name}</TableCell>
      <TableCell className="text-muted-foreground">
        {apiKey.expiresAt ? (
          <span className={isExpired ? 'text-destructive' : ''}>
            {isExpired ? 'Expired ' : ''}
            <RelativeTime date={timestampDate(apiKey.expiresAt)} />
          </span>
        ) : (
          'Never'
        )}
      </TableCell>
      <TableCell className="text-muted-foreground">
        {apiKey.createdAt ? (
          <RelativeTime date={timestampDate(apiKey.createdAt)} />
        ) : (
          '-'
        )}
      </TableCell>
      <TableCell>
        <Button
          variant="destructive"
          size="sm"
          onClick={handleDelete}
          disabled={deleteMutation.isPending}
        >
          {deleteMutation.isPending ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Trash2 className="h-4 w-4" />
          )}
          Delete
        </Button>
      </TableCell>
    </TableRow>
  )
}
