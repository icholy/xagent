# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
mise run build          # Build main binary + prebuilt binaries for linux amd64/arm64
mise run generate       # Generate protobuf code (go tool buf generate)
mise run wipe           # Delete the database
go build                # Build main binary only
```

## Architecture

XAGENT is an async agent orchestrator using a botnet-style C2 (command & control) architecture to run multiple Claude Code instances in parallel inside Docker containers.

### Core Components

- **C2 Server** (`internal/server/`) - Connect RPC API + Web UI, stores tasks and logs in SQLite
- **Runner** (`internal/runner/`) - Polls for pending tasks, manages Docker container lifecycle, creates Unix socket proxy for container-to-server communication
- **Agent** (`internal/agent/`) - Runs inside containers, executes Claude Code CLI (`npx @anthropic-ai/claude-code --print`)
- **Store** (`internal/store/`) - SQLite persistence layer with WAL mode

### Key Concepts

- **Tasks** are the unit of work - contain workspace reference and prompts to execute
- **Agents** run one-to-one with tasks inside containers named `xagent-{task-id}`
- **Workspaces** define container config (image, volumes, env vars) and MCP servers in `workspaces.yaml`
- Communication happens via Unix socket proxy at `/var/run/xagent.sock` inside containers
- Runner auto-injects an `xagent` MCP server (see below)

### MCP Server Tools

The runner injects an `xagent` MCP server into each agent, providing these tools:

- `get_my_task` - Get current task instructions, links, events, and children
- `update_my_task` - Update the current task's name
- `create_link` - Associate external resources (PRs, Jira tickets) with the task
- `report` - Log messages visible in the Web UI
- `create_child_task` - Spawn a child task in the same workspace
- `list_child_tasks` - List child tasks spawned by this task
- `update_child_task` - Add instruction to a child task and restart it
- `list_child_task_logs` - View logs from a child task

### Parent/Child Tasks

Tasks can spawn child tasks to delegate work. The parent task can monitor and interact with its children:

- Child tasks inherit the parent's workspace
- Parent can add instructions to children (triggers restart)
- Parent can read child logs and links
- Tasks track their parent via `parent` field in the database
- Web UI shows child tasks under their parent

### Event System

Tasks can be notified about external events through the event system:

- **Events** represent external triggers (GitHub PR comments, Jira issue updates, etc.)
- **Links** created with `notify=true` route events to tasks when the event URL matches the link URL
- When an event is processed, all tasks with matching notify links receive the event
- Events appear in `get_my_task` output and provide additional context to agents
- External pollers (GitHub, Jira) create events and process them to notify linked tasks

Use `create_link` with `notify=true` for resources that may need follow-up (PRs awaiting review, issues awaiting response, etc.)

### CLI Subcommands

```
xagent server     # Start C2 server
xagent runner     # Start container orchestrator
xagent run        # Run agent (inside container, started by runner)
xagent mcp        # MCP server for tool integration
xagent task       # Task CRUD (list, create, update, delete)
xagent containers # List xagent containers
xagent jira       # Poll Jira for issue comments
xagent github     # GitHub integration
```

### Protobuf

Service definitions in `proto/xagent/v1/xagent.proto`, generated code goes to `internal/proto/` (gitignored).

## Web UI v2

A modern React-based UI is being developed in `webui/` to replace the Go template-based UI in `internal/server/templates/`.

### Stack

- **React 19** with TypeScript
- **Vite** for development and build tooling
- **TanStack Router** for file-based routing
- **TanStack Query** for server state management
- **Tailwind CSS v4** for styling
- **shadcn/ui** for component library

### Development

```bash
cd webui
npm install
npm run dev  # Runs on http://localhost:5173
```

The v2 UI runs independently on the Vite dev server and can be developed incrementally alongside the existing Go template UI.

### Routing (TanStack Router)

**File-based routing** - Routes are defined as files in `src/routes/`. The TanStack Router Vite plugin auto-generates `routeTree.gen.ts` (gitignored).

**Route file naming**:
- `index.tsx` → `/`
- `tasks.tsx` → `/tasks`
- `tasks/$id.tsx` → `/tasks/:id` (dynamic param)
- `tasks.$id.edit.tsx` → `/tasks/:id/edit`
- `__root.tsx` → Root layout (wraps all routes)

**Creating a route**:
```tsx
// src/routes/tasks.tsx
import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/tasks')({
  component: TasksPage,
})

function TasksPage() {
  return <div>Tasks</div>
}
```

**Accessing route params**:
```tsx
// src/routes/tasks/$id.tsx
import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/tasks/$id')({
  component: TaskDetail,
})

function TaskDetail() {
  const { id } = Route.useParams()
  return <div>Task {id}</div>
}
```

**Navigation**:
```tsx
import { Link, useNavigate } from '@tanstack/react-router'

// Declarative
<Link to="/tasks/$id" params={{ id: '123' }}>View Task</Link>

// Programmatic
const navigate = useNavigate()
navigate({ to: '/tasks/$id', params: { id: '123' } })
```

**Type safety** - The router instance is registered via module augmentation in `main.tsx`, enabling full TypeScript autocomplete for routes, params, and navigation throughout the app.

### State Management (TanStack Query)

**Use TanStack Query for ALL server state** - Tasks, logs, events, and any data from the API should be fetched with TanStack Query. Do NOT use `useState` + `useEffect` for API calls.

**QueryClient is available everywhere** - The QueryClient is provided via router context and available in all routes and components.

**Fetching data**:
```tsx
import { useQuery } from '@tanstack/react-query'

function TaskList() {
  const { data: tasks, isLoading, error } = useQuery({
    queryKey: ['tasks'],
    queryFn: async () => {
      const res = await fetch('/xagent.v1.XAgentService/ListTasks', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      return res.json()
    },
    refetchInterval: 5000, // Auto-refresh every 5 seconds
  })

  if (isLoading) return <div>Loading...</div>
  if (error) return <div>Error: {error.message}</div>

  return <div>{/* Render tasks */}</div>
}
```

**Mutations** (create/update/delete):
```tsx
import { useMutation, useQueryClient } from '@tanstack/react-query'

function CreateTaskButton() {
  const queryClient = useQueryClient()

  const mutation = useMutation({
    mutationFn: async (taskData) => {
      const res = await fetch('/xagent.v1.XAgentService/CreateTask', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(taskData),
      })
      return res.json()
    },
    onSuccess: () => {
      // Invalidate and refetch tasks list
      queryClient.invalidateQueries({ queryKey: ['tasks'] })
    },
  })

  return <button onClick={() => mutation.mutate({...})}>Create</button>
}
```

**Benefits**:
- Automatic caching and background updates
- No more polling issues (updates without losing UI state)
- Built-in loading and error states
- Request deduplication
- Optimistic updates

### UI Development Guidelines

**Use shadcn/ui components** - Always prefer off-the-shelf shadcn components with their default styles. Avoid writing custom UI components unless absolutely necessary. Browse available components at https://ui.shadcn.com/docs/components

**Add components as needed** - Install shadcn components with `npx shadcn@latest add <component-name>` (e.g., `button`, `card`, `table`, `dialog`)

**Reference v1 for data, not design** - The Go template UI in `internal/server/templates/` can be referenced to understand:
- What API calls are made
- What data is displayed
- What information appears on each page

**Do NOT copy v1 implementation** - Do not replicate v1's layout, styles, HTML structure, or templates. The v2 is a complete rewrite with modern components and UX patterns.

**API Access** - The v2 UI will call the same Connect RPC API at `/xagent.v1.XAgentService/*` that the v1 UI uses.

### Component Customization

**shadcn/ui philosophy** - shadcn/ui is NOT a traditional npm package. It's a code distribution system that copies component source code into your project (`src/components/ui/`). You own the code and can modify it directly.

**One component, multiple variants** - Do NOT create multiple copies of the same component (e.g., `button-primary.tsx`, `button-secondary.tsx`). Instead, use class-variance-authority (CVA) to add variants to a single component:

```tsx
// src/components/ui/button.tsx
const buttonVariants = cva(
  "inline-flex items-center justify-center...",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground...",
        destructive: "bg-destructive text-destructive-foreground...",
        success: "bg-green-600 text-white hover:bg-green-700", // Custom variant
      }
    }
  }
)
```

Usage: `<Button variant="success">Save</Button>`

**When to create new components**:
- Building composite/wrapper components (e.g., `TaskCard` that uses `Card` + `Badge` + `Button`)
- Creating domain-specific components (e.g., `TaskStatusBadge`)

**Don't create**:
- Multiple variations of the same primitive component
- Wrapper components just to override styles (edit the source directly instead)

**Pattern**: One source of truth per primitive component, compose for complexity.
