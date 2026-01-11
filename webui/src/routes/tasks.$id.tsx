import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/tasks/$id')({
  component: TaskDetail,
})

function TaskDetail() {
  const { id } = Route.useParams()
  return (
    <div className="container mx-auto py-8 px-4">
      <h1 className="text-2xl font-bold">Task {id}</h1>
      <p className="text-muted-foreground mt-4">Task detail page coming soon.</p>
    </div>
  )
}
