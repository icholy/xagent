import { useState, useEffect } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useMutation, useQuery } from '@connectrpc/connect-query'
import { createTask, listWorkspaces } from '@/gen/xagent/v1/xagent-XAgentService_connectquery'
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

const WORKSPACE_STORAGE_KEY = 'xagent-last-workspace'

export const Route = createFileRoute('/tasks/new')({
  component: NewTaskPage,
})

function NewTaskPage() {
  const navigate = useNavigate()

  const [name, setName] = useState('')
  const [workspace, setWorkspace] = useState(() => {
    return localStorage.getItem(WORKSPACE_STORAGE_KEY) || ''
  })
  const [instruction, setInstruction] = useState('')

  const { data: workspacesData } = useQuery(listWorkspaces, {})

  useEffect(() => {
    if (workspacesData?.workspaces && workspace) {
      const workspaceExists = workspacesData.workspaces.some(ws => ws.name === workspace)
      if (!workspaceExists) {
        setWorkspace('')
      }
    }
  }, [workspacesData, workspace])

  const handleWorkspaceChange = (value: string) => {
    setWorkspace(value)
    localStorage.setItem(WORKSPACE_STORAGE_KEY, value)
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
    if (!workspace.trim() || !instruction.trim()) return

    await mutation.mutateAsync({
      name: name.trim(),
      workspace: workspace.trim(),
      parent: 0n,
      instructions: [{ text: instruction.trim(), url: '' }],
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
              <Label htmlFor="workspace">Workspace</Label>
              <Select value={workspace} onValueChange={handleWorkspaceChange} required>
                <SelectTrigger id="workspace">
                  <SelectValue placeholder="Select a workspace" />
                </SelectTrigger>
                <SelectContent>
                  {workspacesData?.workspaces.map((ws) => (
                    <SelectItem key={ws.name} value={ws.name}>
                      {ws.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-sm text-muted-foreground">
                The workspace defines the container configuration for the task
              </p>
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

            {mutation.error && (
              <div className="text-destructive text-sm">
                Error: {mutation.error.message}
              </div>
            )}

            <div className="flex gap-2">
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? 'Creating...' : 'Create Task'}
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => navigate({ to: '/tasks' })}
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
