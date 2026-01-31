import { useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useMutation } from '@connectrpc/connect-query'
import { createWebhook } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export const Route = createFileRoute('/webhooks/new')({
  component: NewWebhookPage,
})

function NewWebhookPage() {
  const navigate = useNavigate()
  const [secret, setSecret] = useState('')

  const mutation = useMutation(createWebhook, {
    onSuccess: () => {
      navigate({ to: '/webhooks' })
    },
  })

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!secret.trim()) return
    await mutation.mutateAsync({ secret: secret.trim() })
  }

  return (
    <div className="container mx-auto py-8 px-4 space-y-6">
      <h1 className="text-2xl font-bold mb-6">Create Webhook</h1>

      <Card>
        <CardContent className="pt-6">
          <form onSubmit={handleSubmit} className="space-y-6">
            <div className="space-y-2">
              <Label htmlFor="secret">Secret</Label>
              <Input
                id="secret"
                type="password"
                placeholder="Enter webhook secret for HMAC verification"
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                required
              />
            </div>

            {mutation.error && (
              <div className="text-destructive text-sm">
                Error: {mutation.error.message}
              </div>
            )}

            <div className="flex gap-2">
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? 'Creating...' : 'Create Webhook'}
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => navigate({ to: '/webhooks' })}
              >
                Cancel
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
