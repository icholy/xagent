import { useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { createTask, listWorkspaces } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
import { useOrgLocalStorage } from '@/hooks/use-org-local-storage'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Card, CardContent } from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { Duration } from '@bufbuild/protobuf/wkt'

export const Route = createFileRoute('/tasks/new')({
  component: NewTaskPage,
})

// durationFromHours parses an integer string number of hours and returns a protobuf
// Duration, or undefined for empty / "never".
function durationFromHours(value: string): Duration | undefined {
  if (!value || value === 'never') return undefined
  const hours = Number.parseInt(value, 10)
  if (!Number.isFinite(hours) || hours <= 0) return undefined
  return { seconds: BigInt(hours * 3600), nanos: 0, $typeName: 'google.protobuf.Duration' }
}

function NewTaskPage() {
  const navigate = useNavigate()

  const [name, setName] = useState('')
  const [runner, setRunner] = useOrgLocalStorage('xagent-last-runner', '')
  const [workspace, setWorkspace] = useOrgLocalStorage('xagent-last-workspace', '')
  const [instruction, setInstruction] = useState('')
  const [archiveAfter, setArchiveAfter] = useState<string>('') // empty = never

  const { data: workspacesData } = useQuery(listWorkspaces, {})

  // Derive unique runners from workspaces
  const runners = [...new Set(workspacesData?.workspaces.map((ws) => ws.runnerId) ?? [])]

  // Filter workspaces by selected runner
  const workspaces = workspacesData?.workspaces.filter((ws) => ws.runnerId === runner) ?? []

  const handleRunnerChange = (newRunner: string) => {
    setRunner(newRunner)
    setWorkspace('')
  }

  const mutation = useMutation(createTask, {
    onSuccess: (data) => {
      if (data.task) {
        navigate({ to: '/tasks/$id', params: { id: String(data.task.id) } })
      } else {
        navigate({ to: '/tasks' })
      }
    },
  })

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!runner.trim() || !workspace.trim() || !instruction.trim()) return

    await mutation.mutateAsync({
      name: name.trim(),
      runner: runner.trim(),
      workspace: workspace.trim(),
      parent: 0n,
      instructions: [{ text: instruction.trim(), url: '' }],
      archiveAfter: durationFromHours(archiveAfter),
    })
  }

  return (
    <div className="container mx-auto py-8 px-4 space-y-6">
      <h1 className="text-2xl font-bold mb-6">Create New Task</h1>

      <Card>
        <CardContent className="pt-6">
          <form onSubmit={handleSubmit} className="space-y-6">
            <div className="space-y-2">
              <Label htmlFor="name">Name (optional)</Label>
              <Input
                id="name"
                placeholder="Enter task name"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="runner">Runner</Label>
              <Select value={runner} onValueChange={handleRunnerChange} required>
                <SelectTrigger id="runner">
                  <SelectValue placeholder="Select a runner" />
                </SelectTrigger>
                <SelectContent>
                  {runners.map((r) => (
                    <SelectItem key={r} value={r}>
                      {r}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="workspace">Workspace</Label>
              <Select value={workspace} onValueChange={setWorkspace} required disabled={!runner}>
                <SelectTrigger id="workspace">
                  <SelectValue
                    placeholder={runner ? 'Select a workspace' : 'Select a runner first'}
                  />
                </SelectTrigger>
                <SelectContent>
                  {workspaces.map((ws) => (
                    <SelectItem key={ws.name} value={ws.name}>
                      <span>{ws.name}</span>
                      {ws.description && (
                        <span className="text-muted-foreground text-xs ml-2">{ws.description}</span>
                      )}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="instruction">Instructions</Label>
              <Textarea
                id="instruction"
                placeholder="Enter the initial instruction for the task..."
                value={instruction}
                onChange={(e) => setInstruction(e.target.value)}
                rows={4}
                required
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="archive-after">Auto-archive after (optional)</Label>
              <Select value={archiveAfter} onValueChange={setArchiveAfter}>
                <SelectTrigger id="archive-after">
                  <SelectValue placeholder="Never (default)" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="never">Never</SelectItem>
                  <SelectItem value="1">1 hour</SelectItem>
                  <SelectItem value="24">24 hours</SelectItem>
                  <SelectItem value="168">7 days</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-muted-foreground text-xs">
                Once the task reaches a terminal status (completed, failed, cancelled), the server
                will archive it after this delay so the container is reclaimed.
              </p>
            </div>

            {mutation.error && (
              <div className="text-destructive text-sm">Error: {mutation.error.message}</div>
            )}

            <div className="flex gap-2">
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? 'Creating...' : 'Create Task'}
              </Button>
              <Button type="button" variant="outline" onClick={() => navigate({ to: '/tasks' })}>
                Cancel
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
