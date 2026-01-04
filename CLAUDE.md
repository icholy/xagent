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
