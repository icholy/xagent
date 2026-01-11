import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/')({
  component: Index,
})

function Index() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background text-foreground">
      <div className="text-center">
        <h1 className="text-4xl font-bold mb-4">XAgent UI v2</h1>
        <p className="text-xl text-muted-foreground">It works!</p>
        <p className="text-sm text-muted-foreground mt-4">
          TanStack Router + TanStack Query + shadcn/ui
        </p>
      </div>
    </div>
  )
}
