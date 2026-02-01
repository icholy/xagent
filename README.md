# XAGENT

An async agent orchestrator that runs multiple Claude Code instances in parallel inside containers. Agents are non-interactive and task-driven, executing prompts like "Implement JIRA ticket X and open a draft PR".

## Quick Start

Install `xagent` cli:

```bash
mise run install
```

Download the pre-built binaries (if needed):

```bash
GITHUB_TOKEN=$(gh auth token) xagent download
```

Authenticate your local client:

```bash
xagent setup
```

Start the local runner:

```bash
xagent runner --concurrency 10 --config workspaces.yaml
```

Create and monitor tasks via the Web UI.

## Claude Code Workspace Example

```yaml
workspaces:
  pets-workshop:
    container:
      image: node:20
      working_dir: /root
      environment:
        CLAUDE_CODE_OAUTH_TOKEN: ${env:CLAUDE_CODE_OAUTH_TOKEN}
    commands:
      - npm install -g @anthropic-ai/claude-code
      - git clone https://github.com/github-samples/pets-workshop
    agent:
      type: claude
      cwd: /root/pets-workshop
      mcp_servers: {}
      prompt: |
        This is an example github repository.
        Don't try opening PRs or issues.
```

## Cursor Agent Workspace Example

```yaml
  pets-workshop:
    container:
      image: node:20
      working_dir: /root
      environment:
        CURSOR_API_KEY: ${env:CURSOR_API_KEY}
    commands:
      - curl -fsSL https://cursor.com/install | bash
      - git clone https://github.com/github-samples/pets-workshop
    agent:
      type: cursor
      cwd: /root/pets-workshop
      mcp_servers: {}
      prompt: |
        This is an example github repository.
        Don't try opening PRs or issues.
```

## MCP Server Workspace Example

```yaml
workspaces:
  pets-workshop:
    # ...
    agent:
      mcp_servers:
        meta:
          type: "http"
          url: "http://metamcp:12008/metamcp/Default/mcp"
          headers:
            Authorization: "Bearer ${env:METAMCP_API_KEY}"
```

## Local Development

```bash
# Start server and postgres locally
docker compose up -d

# Start runner against local server
xagent runner --server http://localhost:6464

# Build
mise run build      # Build main + prebuilt binaries (linux amd64/arm64)
mise run generate   # Generate protobuf code
go build            # Build main binary only

# Run the FE
cd webapp
pnpm run dev
```
