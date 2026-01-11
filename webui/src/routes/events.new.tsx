import { useState } from 'react'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useMutation } from '@connectrpc/connect-query'
import { createEvent } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'

export const Route = createFileRoute('/events/new')({
  component: CreateEventPage,
})

function CreateEventPage() {
  const navigate = useNavigate()
  const [description, setDescription] = useState('')
  const [url, setUrl] = useState('')
  const [data, setData] = useState('')
  const [error, setError] = useState<string | null>(null)

  const mutation = useMutation(createEvent)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)

    try {
      const response = await mutation.mutateAsync({
        description,
        url,
        data,
      })
      if (response.event) {
        navigate({ to: '/events/$id', params: { id: String(response.event.id) } })
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create event')
    }
  }

  return (
    <div className="container mx-auto py-8 px-4 space-y-6">
      <Link to="/events" className="text-primary hover:underline">
        &larr; Back to Events
      </Link>

      <h1 className="text-2xl font-bold">Create Event</h1>

      <Card className="max-w-2xl">
        <CardContent className="pt-6">
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="description">Description</Label>
              <Input
                id="description"
                placeholder="Event description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                required
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="url">URL</Label>
              <Input
                id="url"
                type="url"
                placeholder="https://example.com/resource"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="data">Data (JSON)</Label>
              <Textarea
                id="data"
                placeholder='{"key": "value"}'
                value={data}
                onChange={(e) => setData(e.target.value)}
                rows={6}
              />
            </div>

            {error && (
              <div className="text-destructive text-sm">{error}</div>
            )}

            <div className="flex gap-2">
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? 'Creating...' : 'Create Event'}
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => navigate({ to: '/events' })}
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
