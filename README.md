# XAGENT Design Document

## Overview

XAGENT is an async agent orchestrator that runs multiple Claude Code instances in parallel inside containers. Agents are non-interactive and task-driven, executing prompts like "Implement JIRA ticket X and open a draft PR".

## Architecture

![Architecture](diagrams/architecture.svg)

## Core Concept: Botnet-Style Task Delegation

XAGENT uses a botnet-style C2 (command & control) architecture for agent task delegation:

- **C2 Server** stores tasks, dispatches work, collects logs
- **Runner** polls for pending tasks, spawns agent containers
- **Agents** run in containers, pull tasks, execute prompts, report results
- **Communication** agents connect back to C2 via Unix socket proxy

## Requirements

### Functional
- [ ] MCP server support for tool integration
- [ ] Log collection and review from agent runs
- [x] Programmatic session creation
- [x] Session continuation with additional prompts
- [x] Docker container execution

### Communication Model

Connect RPC over HTTP:
- Agent polls C2 for commands
- Uploads logs in batches
- Simple, stateless, works well with containers

## Container Networking

Containers communicate with the C2 server via a Unix socket proxy:

![Container Networking](diagrams/networking.svg)

- Runner creates a Unix socket proxy at `/tmp/xagent.sock`
- Socket is bind-mounted into containers at `/var/run/xagent.sock`
- Agents connect using `unix:///var/run/xagent.sock`
- Proxy forwards requests to the C2 server

## Components

### 1. C2 Server
- Connect RPC API for task management
- SQLite for task metadata
- Log collection (TODO: not yet implemented)

### 2. Runner
- Standalone process that polls the C2 server for pending tasks
- Creates Unix socket proxy for container communication
- Manages Docker containers
  - Starts containers named `xagent-{task-id}`
  - Copies prebuilt xagent binary into container
  - If container exists, restarts it; otherwise creates new
  - Marks task as `running` when container starts
  - When a prompt is added, task status set to `pending`, runner restarts container

### 3. Agent (Bot)
- Runs Claude Code via ACP
- Connects to C2 via Unix socket
- Executes assigned tasks
- Streams logs back to C2
- Reports task completion/failure

### Tasks vs Agents vs ACP Sessions

There are three distinct concepts:

- **Task** (server-side): The unit of work managed by the C2 server. Contains environment config (docker image, setup commands, MCP servers) and a sequence of prompts to execute.
- **Agent** (runtime): The process running inside a container that executes a Task. One-to-one relationship with Task.
- **ACP Session** (Claude Code): The conversation session created by Claude Code via the ACP protocol. Owned by the agent process.

The C2 server tracks Tasks. Each Task is executed by one Agent process which creates an ACP session.

**Constraint:** If a container is lost, its ACP session history is gone. The agent stores the session ID locally (`~/.xagent/{task-id}.json`) for resumption, but conversation state lives in Claude Code.

### 4. Agent Container
- Base image with Node.js
- Claude Code installed on demand via npx
- Agent binary (prebuilt) copied in at container start
- Minimal footprint

## Data Flow

![Data Flow](diagrams/dataflow.svg)

## CLI

Single binary with subcommands:

```
xagent server        # Start C2 server (API only)
xagent runner        # Start runner (monitors tasks, manages containers)
xagent run           # Run agent process (inside container)
xagent task list     # List tasks
xagent task create   # Create a new task
xagent task update   # Update a task (status, add prompts)
xagent task delete   # Delete a task
xagent containers    # List xagent containers (docker ps wrapper)
```

### `xagent server`

Starts the C2 server (API only, no runner).

```
xagent server [options]

Options:
  -a, --addr <addr>       Listen address (default: ":8080")
  -d, --db <path>         Database file path (default: "xagent.db")
```

### `xagent runner`

Starts the runner process that monitors pending tasks and manages Docker containers.

```
xagent runner [options]

Options:
  -s, --server <url>      C2 server URL (default: "http://localhost:8080")
      --poll <duration>   Poll interval for pending tasks (default: 5s)
      --prebuilt <path>   Directory containing prebuilt binaries (default: "prebuilt")
```

### `xagent run`

Runs an agent process inside a container. Started by the runner.

```
xagent run [options]

Options:
  -s, --server <url>      C2 server URL (default: "http://localhost:8080")
  -t, --task <task_id>    Task ID to execute (required)
  -C, --cwd <path>        Working directory for the agent
```

### `xagent task list`

Lists tasks from the C2 server (JSON output).

```
xagent task list [options]

Options:
  -s, --server <url>      C2 server URL (default: "http://localhost:8080")
      --status <status>   Filter by status (pending, running, completed, failed)
```

### `xagent task create`

Creates a new task (JSON output).

```
xagent task create [options]

Options:
  -s, --server <url>      C2 server URL (default: "http://localhost:8080")
      --id <id>           Task ID (optional, auto-generated if not provided)
  -i, --image <image>     Docker image to run the agent in (default: "node:alpine")
  -c, --command <cmd>     Setup command (can be specified multiple times)
  -p, --prompt <text>     Prompt to execute (can be specified multiple times)
```

### `xagent task update`

Updates a task.

```
xagent task update <task-id> [options]

Options:
  -s, --server <url>      C2 server URL (default: "http://localhost:8080")
      --status <status>   Set task status (pending, running, completed, failed)
  -p, --add-prompt <text> Add prompt to task (can be specified multiple times)
```

### `xagent task delete`

Deletes a task.

```
xagent task delete <task-id> [options]

Options:
  -s, --server <url>      C2 server URL (default: "http://localhost:8080")
```

### `xagent containers`

Lists xagent containers (wrapper around `docker ps`).

```
xagent containers
```

### Usage Examples

```bash
# Start server (API only)
xagent server --addr :9000

# Start runner (in separate process)
xagent runner --server http://localhost:9000

# Manual: run agent for a specific task (normally done by runner)
xagent run --server unix:///var/run/xagent.sock --task abc-123

# List all tasks
xagent task list --server http://localhost:9000

# List only running tasks
xagent task list --status running

# Create a task
xagent task create --image node:20 --prompt "Implement JIRA-123"

# Add a follow-up prompt
xagent task update abc-123 --add-prompt "Also add tests"

# Delete a task
xagent task delete abc-123

# List containers
xagent containers
```

## Authentication

The Claude Agent SDK supports two authentication methods:

| Env Var | Source | Billing |
|---------|--------|---------|
| `ANTHROPIC_API_KEY` | [console.anthropic.com](https://console.anthropic.com) | Pay-per-use |
| `CLAUDE_CODE_OAUTH_TOKEN` | `claude setup-token` | Subscription (Pro/Max) |

**Important:** Do not set both environment variables. The SDK prioritizes `ANTHROPIC_API_KEY` over `CLAUDE_CODE_OAUTH_TOKEN`. If both are set and `ANTHROPIC_API_KEY` is invalid, you'll get auth errors even with a valid OAuth token.

To use a Claude subscription:
```bash
# Generate token (interactive, opens browser)
claude setup-token

# Use in container
export CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-...
```

## Storage

SQLite database at project root:

```
xagent.db              # SQLite database
```

### Tasks Table

```sql
CREATE TABLE tasks (
    id            TEXT PRIMARY KEY,
    image         TEXT NOT NULL,
    commands      TEXT NOT NULL,  -- JSON array of setup commands
    mcp_servers   TEXT NOT NULL,  -- JSON array of MCP server configs
    prompts       TEXT NOT NULL,  -- JSON array of prompts
    status        TEXT NOT NULL,  -- pending, running, completed, failed
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

A **Task** is the complete unit of work:
- `image`: Container image to run the agent in
- `commands`: Setup commands executed before the agent starts prompting
- `mcp_servers`: MCP server configurations for tool access
- `prompts`: Array of prompts to execute in sequence

## API Design (Connect RPC)

Protocol Buffers definition at `proto/xagent/v1/xagent.proto`:

```protobuf
service XAgentService {
  rpc ListTasks(ListTasksRequest) returns (ListTasksResponse);
  rpc CreateTask(CreateTaskRequest) returns (CreateTaskResponse);
  rpc GetTask(GetTaskRequest) returns (GetTaskResponse);
  rpc UpdateTask(UpdateTaskRequest) returns (UpdateTaskResponse);
  rpc DeleteTask(DeleteTaskRequest) returns (DeleteTaskResponse);
  rpc UploadLogs(UploadLogsRequest) returns (UploadLogsResponse);
}
```

Generated Go code is placed in `internal/proto/` (gitignored).

### Agent Local State

The agent stores its local state in `~/.xagent/{task-id}.json`:

```json
{
  "session_id": "acp-session-uuid",
  "prompt_index": 0
}
```

This keeps session info and progress local to the agent. If the container restarts, it can resume where it left off.

### Task Lifecycle

1. Client creates task: `CreateTask` with image, commands, mcp_servers, prompts (status: `pending`)
2. Runner detects pending task via `ListTasks(status: "pending")`
3. Runner starts container `xagent-{task-id}` with `xagent run --task {id}`
4. Runner marks task as `running` via `UpdateTask`
5. Agent runs setup commands
6. Agent calls `GetTask` to get task details (including prompts)
7. Agent creates ACP session, stores state locally in `~/.xagent/{task-id}.json`
8. Agent executes `prompts[prompt_index]`, increments local `prompt_index`
9. Agent uploads logs via `UploadLogs`
10. Agent polls `GetTask` for new prompts
11. When done, agent exits and marks task as `completed`

**Follow-up prompts:**
1. Client adds prompt: `UpdateTask` with `add_prompts` → server sets status to `pending`
2. Runner detects pending status, restarts container `xagent-{task-id}`
3. Agent resumes from local state, executes new prompt

## Build

Prebuilt binaries for containers:

```bash
# Build for all architectures
mise run build-prebuilt

# Generates:
# prebuilt/xagent-linux-amd64
# prebuilt/xagent-linux-arm64
```

Generate protobuf code:

```bash
mise run generate
# or
go tool buf generate
```

## Open Questions

1. **Scaling**: How many concurrent agents?
   - Resource limits per container
   - API rate limiting considerations

2. **Security**:
   - Agent authentication to C2
   - Secrets management (API keys)
   - Network isolation

3. **MCP Servers**:
   - Run MCP servers in agent container?
   - Shared MCP servers on network?
   - Pass MCP config from C2?

4. **Git/GitHub access**:
   - How do agents authenticate to push branches/PRs?
   - SSH keys? GitHub App? PAT?
