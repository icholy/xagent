# XAGENT Design Document

## Overview

XAGENT is an async agent orchestrator that runs multiple Claude Code instances in parallel inside containers. Agents are non-interactive and task-driven, executing prompts like "Implement JIRA ticket X and open a draft PR".

## Architecture

![Architecture](diagrams/architecture.svg)

## Core Concept: Botnet-Style Task Delegation

XAGENT uses a botnet-style C2 (command & control) architecture for agent task delegation:

- **C2 Server** stores tasks, dispatches work, collects logs
- **Runner** polls for pending tasks, spawns agent containers, monitors container exits
- **Agents** run in containers, pull tasks, execute prompts via Claude CLI, report results
- **Communication** agents connect back to C2 via Unix socket proxy

## Requirements

### Functional
- [x] MCP server support for tool integration
- [x] Log collection and review from agent runs
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
- SQLite for task metadata and logs
- Web UI for task monitoring

### 2. Runner
- Standalone process that polls the C2 server for pending tasks
- Creates Unix socket proxy for container communication
- Manages Docker containers:
  - Starts containers named `xagent-{task-id}`
  - Copies prebuilt xagent binary into container
  - If container exists, restarts it; otherwise creates new
  - Marks task as `running` when container starts
  - When a prompt is added, task status set to `pending`, runner restarts container
- **Reconciliation**: On startup, checks for exited containers and updates task status accordingly (handles runner restarts)
- **Monitoring**: Watches for container `die` events and updates task status based on exit code
- **Cancellation**: Kills containers for cancelled tasks and marks them as failed

### 3. Agent (Bot)
- Runs Claude Code via CLI (`npx @anthropic-ai/claude-code --print`)
- Connects to C2 via Unix socket
- Executes assigned tasks
- Uses `--continue` flag to resume sessions for follow-up prompts
- **MCP Proxy**: The runner automatically injects an `xagent` MCP server into the agent's config. This MCP server (`xagent mcp`) provides tools that Claude can use during execution:
  - `create_link` - Link external resources (PRs, Jira issues) to the task
  - `report` - Log messages visible in the Web UI

### Tasks vs Agents vs Sessions

There are three distinct concepts:

- **Task** (server-side): The unit of work managed by the C2 server. Contains workspace reference and a sequence of prompts to execute.
- **Agent** (runtime): The process running inside a container that executes a Task. One-to-one relationship with Task.
- **Session** (Claude Code): The conversation session created by Claude Code. Owned by the agent process and resumed with `--continue`.

The C2 server tracks Tasks. Each Task is executed by one Agent process which creates a Claude Code session.

**Constraint:** If a container is lost, its session history is gone. The agent stores state locally (`~/.xagent/{task-id}.json`) for resumption.

### 4. Agent Container
- Base image with Node.js
- Claude Code installed on demand via npx
- Agent binary (prebuilt) copied in at container start
- Sets `XAGENT_TASK_ID` environment variable
- Minimal footprint

## Data Flow

![Data Flow](diagrams/dataflow.svg)

## CLI

Single binary with subcommands:

```
xagent server        # Start C2 server (API + Web UI)
xagent runner        # Start runner (monitors tasks, manages containers)
xagent run           # Run agent process (inside container)
xagent mcp           # Start MCP server (provides tools to agents)
xagent task list     # List tasks
xagent task create   # Create a new task
xagent task update   # Update a task (status, add prompts)
xagent task delete   # Delete a task
xagent containers    # List xagent containers (docker ps wrapper)
xagent jira          # Poll Jira for issue comments
```

### `xagent server`

Starts the C2 server (API + Web UI).

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
  -c, --config <path>     Workspace config file (default: "workspaces.yaml")
      --poll <duration>   Poll interval for pending tasks (default: 5s)
      --prebuilt <path>   Directory containing prebuilt binaries (default: "prebuilt")
      --debug             Stream container logs to stdout/stderr
```

### `xagent run`

Runs an agent process inside a container. Started by the runner.

```
xagent run [options]

Options:
  -s, --server <url>      C2 server URL (default: "http://localhost:8080")
  -t, --task <task_id>    Task ID to execute (required)
```

### `xagent mcp`

Starts an MCP server that provides tools to agents. Automatically injected by the runner.

```
xagent mcp [options]

Options:
  -s, --server <url>      C2 server URL (default: "http://localhost:8080")
  -t, --task <task_id>    Task ID for context (required)
```

Provides tools:
- `create_link`: Create a link between the task and an external resource (e.g., PR URL, Jira issue)
- `report`: Log a message for the task (displayed in Web UI)

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
  -w, --workspace <name>  Workspace name (required)
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

### `xagent jira`

Polls Jira for issue comments and creates/updates tasks.

```
xagent jira [options]

Options:
  -s, --server <url>      C2 server URL (default: "http://localhost:8080")
      --label <label>     Label to filter issues by (default: "xagent")
      --interval <dur>    Poll interval (default: 30s)
      --url <url>         Jira base URL (env: JIRA_BASE_URL)
  -u, --username <email>  Jira username/email (env: JIRA_USERNAME)
      --token <token>     Jira API token (env: JIRA_API_TOKEN)
  -d, --data <dir>        Data directory for state persistence (default: "data")
  -w, --workspace <name>  Workspace for new tasks (required)
```

Comment commands:
- `xagent new` - Create a new task linked to the issue
- `xagent task` - Add a prompt to an existing linked task

### Usage Examples

```bash
# Start server (API + Web UI)
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
xagent task create --workspace myworkspace --prompt "Implement JIRA-123"

# Add a follow-up prompt
xagent task update abc-123 --add-prompt "Also add tests"

# Delete a task
xagent task delete abc-123

# List containers
xagent containers

# Start Jira poller
xagent jira --workspace myworkspace --url https://company.atlassian.net
```

## Workspaces Configuration

Workspaces are defined in `workspaces.yaml`:

```yaml
myworkspace:
  # Setup commands run before agent starts
  commands:
    - git clone https://github.com/org/repo /workspace

  # Container configuration
  container:
    image: node:20
    working_dir: /workspace
    user: "1000:1000"  # Optional: run as specific UID:GID
    volumes:
      - /host/path:/container/path
    networks:
      - mynetwork
    group_add:
      - docker
    environment:
      MY_VAR: value

  # Agent configuration
  agent:
    cwd: /workspace  # Supports $XAGENT_TASK_ID expansion
    mcp_servers:
      filesystem:
        type: stdio
        command: npx
        args: ["-y", "@anthropic-ai/mcp-filesystem", "/workspace"]
```

The runner automatically injects an `xagent` MCP server alongside any user-defined servers. This provides Claude with `create_link` and `report` tools for task communication.

## Authentication

The Claude CLI supports two authentication methods:

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
    workspace     TEXT NOT NULL,
    prompts       TEXT NOT NULL,  -- JSON array of prompts
    status        TEXT NOT NULL,  -- pending, running, completed, failed, cancelled, archived
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Links Table

```sql
CREATE TABLE links (
    id         TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL,
    type       TEXT NOT NULL,     -- e.g., "github", "jira"
    url        TEXT NOT NULL,
    title      TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Logs Table

```sql
CREATE TABLE logs (
    id         TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL,
    type       TEXT NOT NULL,     -- "info", "error", "llm"
    content    TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

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
  rpc CreateLink(CreateLinkRequest) returns (CreateLinkResponse);
  rpc FindLinksByURL(FindLinksByURLRequest) returns (FindLinksByURLResponse);
}
```

Generated Go code is placed in `internal/proto/` (gitignored).

### Agent Local State

The agent stores its local state in `~/.xagent/{task-id}.json`:

```json
{
  "cwd": "/workspace",
  "mcp_servers": {},
  "commands": [],
  "started": true,
  "prompt_index": 1
}
```

- `started`: Whether the Claude session has been started (use `--continue` if true)
- `prompt_index`: Number of prompts already executed

### Task Lifecycle

1. Client creates task: `CreateTask` with workspace, prompts (status: `pending`)
2. Runner detects pending task via `ListTasks(status: "pending")`
3. Runner starts container `xagent-{task-id}` with `xagent run --task {id}`
4. Runner marks task as `running` via `UpdateTask`
5. Agent runs setup commands from workspace config
6. Agent calls `GetTask` to get task details (including prompts)
7. Agent creates Claude session, stores state locally in `~/.xagent/{task-id}.json`
8. Agent executes `prompts[prompt_index]` via `npx @anthropic-ai/claude-code --print`
9. Agent increments local `prompt_index`, sets `started: true`
10. Agent polls `GetTask` for new prompts
11. When done, agent exits (exit code 0 = completed, non-zero = failed)
12. Runner Monitor detects container exit and updates task status

**Follow-up prompts:**
1. Client adds prompt: `UpdateTask` with `add_prompts` → server sets status to `pending`
2. Runner detects pending status, restarts container `xagent-{task-id}`
3. Agent resumes with `--continue`, executes new prompt

**Runner Restart:**
1. Runner calls `Reconcile()` on startup
2. Finds exited containers with tasks still marked "running"
3. Inspects container exit code and updates task status

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

3. **Git/GitHub access**:
   - How do agents authenticate to push branches/PRs?
   - SSH keys? GitHub App? PAT?
