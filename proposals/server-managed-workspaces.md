# Server-Managed Workspace Configurations

Status: pending
Issue: #397

## Problem

Deploying a runner requires a `workspaces.yaml` file on the host. The runner loads this file at startup to get the full workspace configuration (container settings, agent config, volumes, env vars, MCP servers). This means you can't deploy a runner with just a Docker Compose file and environment variables -- the config file must be created, mounted, and maintained separately.

## Design

### Store raw YAML on the server

Add a `workspace_configs` table that stores the complete `workspaces.yaml` content as a single text blob per org. The runner pulls this on startup instead of reading a local file.

Raw YAML (not structured proto) because:
- The workspace config schema changes frequently (new agent types, container options, MCP configs)
- Variable expansion (`${env:VAR}`, `${sh:command}`) happens runner-side, so the server doesn't need to understand the structure
- Avoids maintaining parallel proto definitions that mirror the YAML schema

### Database Schema

New migration `015_workspace_configs.sql`:

```sql
CREATE TABLE workspace_configs (
    id         BIGSERIAL PRIMARY KEY,
    org_id     BIGINT NOT NULL UNIQUE REFERENCES orgs(id),
    content    TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

One row per org. The `content` column holds the full YAML.

### API Changes

Two new RPCs in `xagent.proto`:

```proto
rpc PushWorkspaceConfig(PushWorkspaceConfigRequest) returns (PushWorkspaceConfigResponse);
rpc PullWorkspaceConfig(PullWorkspaceConfigRequest) returns (PullWorkspaceConfigResponse);

message PushWorkspaceConfigRequest {
  string content = 1;
}

message PushWorkspaceConfigResponse {}

message PullWorkspaceConfigRequest {}

message PullWorkspaceConfigResponse {
  string content = 1;
}
```

Org context comes from auth, same as all other endpoints.

### Store Methods

Add to `internal/store/`:

```go
func (s *Store) UpsertWorkspaceConfig(ctx context.Context, tx *sql.Tx, orgID int64, content string) error
func (s *Store) GetWorkspaceConfig(ctx context.Context, tx *sql.Tx, orgID int64) (string, error)
```

SQL queries:

```sql
-- name: UpsertWorkspaceConfig :exec
INSERT INTO workspace_configs (org_id, content, updated_at)
VALUES ($1, $2, CURRENT_TIMESTAMP)
ON CONFLICT (org_id) DO UPDATE SET content = $2, updated_at = CURRENT_TIMESTAMP;

-- name: GetWorkspaceConfig :one
SELECT content FROM workspace_configs WHERE org_id = $1;
```

### Server Handlers

`PushWorkspaceConfig` validates the YAML parses correctly using `workspace.ParseConfig()` (new function, like `LoadConfig` but takes `[]byte` and skips variable expansion), then stores it. `PullWorkspaceConfig` returns the raw content.

### CLI Commands

Add subcommands to the existing `xagent workspaces` command group (or create it if it doesn't exist):

```
xagent workspaces push [--config path]   # Upload local YAML to server
xagent workspaces pull [--output path]   # Download server YAML to local file or stdout
```

- `push` reads a local file (default `~/.config/xagent/workspaces.yaml`), validates it parses, then uploads via `PushWorkspaceConfig`
- `pull` downloads via `PullWorkspaceConfig` and writes to file or stdout

### Runner Behavior Changes

Update the startup flow in `internal/command/runner.go`:

1. If `--workspaces` flag is explicitly set, load from local file (current behavior, acts as override)
2. Otherwise, call `PullWorkspaceConfig` to fetch from server
3. If server returns empty/not found, fall back to local default path (existing `DefaultPath()` behavior)
4. Parse YAML with `workspace.LoadConfig`, expand variables, proceed with `RegisterWorkspaces` as today

This means a runner deployed via Docker Compose only needs `XAGENT_SERVER` and `XAGENT_API_KEY` environment variables. The workspace config is pulled from the server automatically.

### Variable Expansion

Variable expansion continues to happen runner-side after pulling the config. The server stores the template with `${env:VAR}` references intact. Different runners can resolve different values from their local environment. No secrets are stored on the server.

### Interaction with Existing Registration

The `RegisterWorkspaces` RPC and `workspaces` table remain unchanged. They track which workspaces are currently available on which runners. The new `workspace_configs` table stores the canonical configuration for an org. The flow becomes:

1. Config is pushed to server via CLI (or future UI)
2. Runner pulls config from server on startup
3. Runner parses YAML and expands variables locally
4. Runner registers workspace names + descriptions via `RegisterWorkspaces` (existing flow)
5. Runner uses the parsed config for container creation (existing flow)

## Trade-offs

| Approach | Pros | Cons |
|----------|------|------|
| **Raw YAML blob** (proposed) | Simple, schema-agnostic, preserves existing format, no proto drift | No server-side structured validation, no granular queries |
| **Structured proto fields** | Server can validate fields, UI can render forms | Proto must mirror YAML schema, constant drift as config evolves |
| **Per-workspace storage** | Granular updates, selective pull | More complex schema, harder to manage as a unit |

The raw YAML approach is chosen because the runner is already the authority on parsing and validating workspace config. The server just needs to store and serve it.

## Open Questions

1. **Hot reload**: Should the runner periodically re-pull config from the server, or only load at startup? Periodic refresh would let you update workspaces without restarting runners, but adds complexity around when to apply changes (only to new tasks, not running ones).

2. **Server-side validation**: Should `PushWorkspaceConfig` just check that the YAML parses, or also validate the config (e.g. image is set)? Full validation couples the server to the config schema. Parse-only validation catches syntax errors without that coupling.

3. **Migration path**: Should the runner auto-push its local config to the server on first startup if no server config exists? This would bootstrap the system without requiring a manual `xagent workspaces push`.
