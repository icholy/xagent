import { useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useQuery, useMutation } from '@connectrpc/connect-query'
import {
  listWebhooks,
  deleteWebhook,
} from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import type { Webhook } from '@/gen/xagent/v1/xagent_pb'
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
import { Plus, Trash2, Loader2, Copy, Check } from 'lucide-react'

export const Route = createFileRoute('/webhooks/')({
  component: WebhooksPage,
})

function WebhooksPage() {
  const { data, isLoading, error, refetch } = useQuery(listWebhooks, {}, {
    refetchInterval: 6000,
  })

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <div className="text-muted-foreground">Loading webhooks...</div>
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

  const webhooks = data?.webhooks ?? []

  return (
    <div className="container mx-auto py-8 px-4">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Webhooks</h1>
        <Link to="/webhooks/new">
          <Button>
            <Plus className="h-4 w-4" />
            Webhook
          </Button>
        </Link>
      </div>
      {webhooks.length === 0 ? (
        <div className="text-muted-foreground text-center py-8">
          No webhooks registered
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Endpoint</TableHead>
              <TableHead>Created</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {webhooks.map((webhook) => (
              <WebhookRow
                key={webhook.uuid}
                webhook={webhook}
                onDelete={refetch}
              />
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

function WebhookRow({
  webhook,
  onDelete,
}: {
  webhook: Webhook
  onDelete: () => void
}) {
  const [copied, setCopied] = useState(false)
  const deleteMutation = useMutation(deleteWebhook, {
    onSuccess: () => onDelete(),
  })

  const handleDelete = async () => {
    await deleteMutation.mutateAsync({ uuid: webhook.uuid })
  }

  const endpoint = `/webhook/${webhook.uuid}`

  const handleCopy = async () => {
    await navigator.clipboard.writeText(endpoint)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <TableRow>
      <TableCell>
        <div className="flex items-center gap-2">
          <code className="text-sm bg-muted px-2 py-1 rounded">{endpoint}</code>
          <Button variant="ghost" size="icon-sm" onClick={handleCopy}>
            {copied ? (
              <Check className="h-4 w-4 text-green-500" />
            ) : (
              <Copy className="h-4 w-4" />
            )}
          </Button>
        </div>
      </TableCell>
      <TableCell className="text-muted-foreground">
        {webhook.createdAt ? (
          <RelativeTime date={timestampDate(webhook.createdAt)} />
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
