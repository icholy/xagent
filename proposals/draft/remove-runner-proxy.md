# Remove the Runner Proxy: Driver Talks to the Server Directly

Issue: https://github.com/icholy/xagent/issues/890

## Problem

In-container agents never reach the C2 server directly. The runner stands up a
Unix socket proxy and acts as an authenticating man-in-the-middle:

- **Socket creation** — `xagentclient.NewUnixProxy` (`internal/xagentclient/unix.go`)
  listens on a Unix socket; the runner serves a Connect handler on it from
  `AgentProxy.Start` (`internal/runner/proxy.go:48`). The socket path is
  `/xagent/socket` inside the container (`internal/xagentclient/client.go:22`).
- **Delivery into the container** — the runner bind-mounts the socket's *parent
  directory* (not the file, so the inode survives runner restarts) into the
  container and sets `XAGENT_SERVER=unix:///xagent/socket`
  (`internal/runner/runner.go`, the `Binds` / `Env` blocks). The driver and the
  injected `xagent` MCP server are launched with `--server unix:///xagent/socket`.
- **How the client dials it** — `xagentclient.New` recognizes the `unix://`
  prefix and installs a `DialContext` that dials the socket
  (`internal/xagentclient/client.go:50`). `AuthTransport` adds
  `Authorization: Bearer <task-jwt>` to every request
  (`internal/xagentclient/transport.go`).
- **Token minting** — `AgentProxy.TaskToken` signs a `TaskClaims` JWT with the
  runner's Ed25519 key (`internal/runner/proxy.go:82`). Claims today:
  `TaskID`, `Workspace`, `Runner`, `Scopes` (`internal/auth/agentauth/token.go:32`).
  There is **no `exp`** — these tokens are effectively immortal. The runner
  injects the token via `--token` and `XAGENT_TOKEN`.
- **Authorization (the "runner filter")** — `agentauth.Middleware` verifies the
  JWT against the runner key and puts `TaskClaims` in context
  (`internal/auth/agentauth/middleware.go`). `agentmcp.AgentFilter`
  (`internal/agentmcp/filter.go`) then enforces, per RPC:

  | RPC | Rule enforced today |
  |---|---|
  | `Ping` | always allowed |
  | `CreateLink` | `req.TaskId == claims.TaskID` |
  | `UploadLogs` | `req.TaskId == claims.TaskID` |
  | `SubmitRunnerEvents` | every `ev.TaskId == claims.TaskID` |
  | `GetTask` / `GetTaskDetails` | own task, or a **direct** child (`task.Parent == claims.TaskID`) with `child_tasks` scope |
  | `UpdateTask` | own task, or direct child with `child_tasks` scope; rejects archived |
  | `ListLogs` | own task, or direct child with `child_tasks` scope |
  | `CreateTask` | requires `child_tasks`; `Parent == claims.TaskID`, same `Workspace` and `Runner` |
  | `ListChildTasks` | requires `child_tasks`; `ParentId == claims.TaskID` |
  | `CreateGitHubToken` | requires `github_token` scope |

  The filter then forwards the call to the real server using the runner's
  client, which is authenticated with the runner's **org-scoped API key**. So
  today **org scoping comes from the runner's credential** and **task scoping
  comes from `AgentFilter`** — two separate mechanisms stitched together in the
  runner process.

The server itself (`internal/server/server.go:88`) only knows two caller
shapes, both produced by `apiauth`: a `UserInfo` from a cookie session, an
`xat_` API key, or an app JWT (`internal/auth/apiauth/apiauth.go:204`). Every
`apiserver` handler scopes by `apiauth.Caller(ctx).OrgID`. The server has **no
concept of a task as a caller** and cannot verify a task JWT (it never sees the
runner's key).

This MITM design couples every container's connectivity to a live, co-located
runner and a host bind mount; duplicates org+task authorization out of process;
and runs two disjoint signing keys. We want to delete the proxy and the filter
and let the driver authenticate to the server on its own.

## Design

### Overview

1. The **server** becomes the sole minter and verifier of task tokens, signing
   them with the same `appKey` it already uses for app JWTs. The token gains the
   claims the server needs to scope a task caller: `org_id` plus the existing
   task identity.
2. The **runner** stops signing tokens and stops running the proxy. It requests
   a task token from the server (over its existing authenticated client) and
   injects it into the container alongside the server's public URL.
3. The **server** authenticates task tokens into a new task-scoped caller, and a
   single server-side interceptor enforces everything `AgentFilter` does today,
   using the store as the source of truth for task lineage.
4. The driver and MCP server dial the server's public HTTPS endpoint directly.
   The proxy, the filter, and the runner's signing key are deleted.

```
BEFORE                                  AFTER
driver ──unix──▶ runner proxy           driver ──https──▶ C2 server
                  │ agentauth.Middleware                   │ apiauth task-token auth
                  │ AgentFilter (task scope)               │ TaskScopeInterceptor (task scope)
                  └─https(runner API key)─▶ C2 server      └─ apiserver handlers (org scope)
```

### Token claims

Extend the task token so the server can scope it without consulting the runner.
`TaskClaims` (`internal/auth/agentauth/token.go`) becomes:

```go
type TaskClaims struct {
    jwt.RegisteredClaims        // Subject = "task:<id>", IssuedAt, ExpiresAt (set!)
    TaskID    int64    `json:"task_id"`
    OrgID     int64    `json:"org_id"`     // NEW — replaces the runner API key's org scoping
    Workspace string   `json:"workspace"`
    Runner    string   `json:"runner"`
    Scopes    []string `json:"scopes,omitempty"`
}
```

Notes on the claim set:

- **`org_id` is the key addition.** Today the runner's API key supplies the org;
  with a direct connection the token must carry it so `apiserver` handlers keep
  scoping by `caller.OrgID` unchanged.
- **Parent lineage is intentionally *not* in the token.** The filter's
  parent/child checks need the *current* `task.Parent`, which can change and
  must not be trusted from a long-lived token. The server already has the store;
  it resolves lineage authoritatively per request (see below). Keeping lineage
  out of the token also means a child created after the token was minted is still
  reachable.
- **Scopes** are unchanged in meaning (`child_tasks`, `github_token`).
- A distinct `Subject`/audience (e.g. `aud: "task"`) lets `apiauth` tell a task
  token apart from an app token without trial-parsing.

**Who mints it.** The server. Add an RPC:

```proto
// CreateTaskToken issues a short-lived app token scoped to a single task.
// Caller must be the runner that owns the task (org-scoped API key / app JWT).
rpc CreateTaskToken(CreateTaskTokenRequest) returns (CreateTaskTokenResponse);

message CreateTaskTokenRequest {
  int64 task_id = 1;
  repeated string scopes = 2;   // workspace-granted scopes, validated server-side
}
message CreateTaskTokenResponse {
  string token = 1;
  int64 expires_at = 2;         // unix seconds
}
```

The handler loads the task, checks `task.OrgID == caller.OrgID` and
`task.Runner == caller`'s runner, intersects the requested scopes with what the
workspace is allowed, and signs `TaskClaims` with `appKey`. Because the server
both signs and verifies with `appKey`, no key distribution is needed — the
runner's Ed25519 signing key (`cfg.PrivateKey`, `--private-key`) is **deleted**.

**Lifetime.** App JWTs live 5 minutes; that is far too short for a long-running
agent. Task tokens get a moderate TTL — proposed **24h** (`TaskTokenTTL`) — and
the runner re-mints on each container (re)start (it already re-injects env/args
on start and SIGHUP-reload per the driver-owned-events work). For tasks that may
outlive the TTL, the driver refreshes by calling `CreateTaskToken` again before
expiry using its current (still-valid) token; `CreateTaskToken` therefore also
accepts a task-token caller scoped to *its own* `task_id`. See Open Questions.

**Delivery into the container.** Unchanged shape, new values: the runner sets
`XAGENT_SERVER=<public server URL>` (instead of `unix:///xagent/socket`) and
`XAGENT_TOKEN=<task token>`, and passes `--server <url> --token <token>` to both
`driver` and `tool agent-mcp`. The `unix://` dialing branch in `xagentclient`
and the socket bind mount are removed.

### Server-side enforcement

**Authentication.** Extend `apiauth.authenticate` (`internal/auth/apiauth/apiauth.go:204`)
to recognize a task token (by `aud`/subject) and produce a task-scoped caller.
Rather than overload `UserInfo`, attach task claims in context and reuse
`UserInfo` only for the org so existing handlers keep working:

```go
// after VerifyAppToken fails / by audience:
claims, err := agentauth.VerifyToken(a.appKey, raw)   // now verified with the server key
if err != nil { return nil, err }
ctx = agentauth.ContextWithClaims(ctx, claims)
return &UserInfo{
    ID:    claims.Subject,         // "task:<id>"
    OrgID: claims.OrgID,           // drives existing org scoping
    Type:  AuthTypeTask,           // NEW
}, nil
```

This single change makes every org-scoped `apiserver` handler behave correctly
for a task caller — a task can only ever see its own org's rows. What remains is
the *intra-org* task isolation that `AgentFilter` does.

**Authorization — `TaskScopeInterceptor`.** Add a Connect interceptor, installed
after auth in `server.go`, that is a no-op for non-task callers and, for task
callers, ports `AgentFilter` verbatim — same checks, but using the store for
lineage instead of an upstream `GetTask`:

```go
path, handler := xagentv1connect.NewXAgentServiceHandler(s.api,
    connect.WithInterceptors(
        otelInterceptor,
        apiauth.RequireUserInterceptor(),
        apiauth.TaskScopeInterceptor(s.store),   // NEW
    ),
)
```

The interceptor type-switches on the request message (exactly as the filter's
per-RPC methods do) and enforces:

- **Allowlist.** A task caller may *only* invoke: `Ping`, `CreateLink`,
  `UploadLogs`, `SubmitRunnerEvents`, `GetTask`, `GetTaskDetails`, `UpdateTask`,
  `ListLogs`, `CreateTask`, `ListChildTasks`, `CreateGitHubToken`,
  `CreateTaskToken` (self-refresh). Every other RPC (org admin, key management,
  routing rules, workspace registration, …) returns `PermissionDenied`. This is
  the critical inversion: today the proxy exposes a *handler that implements only
  these methods*; on the server the full service is reachable, so the allowlist
  must be explicit and default-deny.
- **Self-scoping** for `CreateLink` / `UploadLogs` / `SubmitRunnerEvents`:
  `req.TaskId == claims.TaskID`.
- **Parent/child** for `GetTask` / `GetTaskDetails` / `UpdateTask` / `ListLogs`:
  allowed if `id == claims.TaskID`, or if `store.GetTask(id).Parent ==
  claims.TaskID` **and** `claims.HasScope(child_tasks)`. `UpdateTask` keeps the
  "reject archived" rule.
- **Child creation** for `CreateTask`: requires `child_tasks`,
  `req.Parent == claims.TaskID`, `req.Workspace == claims.Workspace`,
  `req.Runner == claims.Runner`, and (new, since org now comes from the token)
  the created task inherits `claims.OrgID`.
- **`ListChildTasks`**: requires `child_tasks`, `req.ParentId == claims.TaskID`.
- **`CreateGitHubToken`**: requires `github_token`.

Because the checks now run *inside* the server with direct store access, the
double round-trips the filter makes today (e.g. `UpdateTask` doing a `GetTask`
first) become local store reads.

### Direct connectivity

- **Address.** The runner injects the server's reachable URL. In the hosted
  deployment this is the public HTTPS endpoint (`xagentclient.DefaultURL`,
  `https://xagent.choly.ca`); in self-hosted setups it is whatever address the
  runner already uses (`--server`), provided containers can route to it. Add a
  runner flag/config `--agent-server-url` defaulting to the runner's own
  `--server` so operators can override when the container-visible address
  differs from the runner-visible one (NAT, internal DNS).
- **TLS.** The public endpoint already terminates TLS; the `unix://` path had no
  encryption because it was host-local. Direct connections use the same HTTPS
  the runner and web UI use — no new TLS material.
- **Network reachability.** Containers must have egress to the server. Today
  agent containers already reach the public internet (GitHub, package
  registries, the LLM API), so egress to the C2 endpoint is normally available.
  This is the one genuinely new requirement and must be called out for
  air-gapped/locked-down deployments (see Risks).

### What gets deleted

- `internal/runner/proxy.go` — the entire `AgentProxy` (socket, middleware,
  token minting).
- `internal/agentmcp/filter.go` — `AgentFilter` (logic moves into
  `TaskScopeInterceptor`).
- `internal/xagentclient/unix.go` — `UnixProxy`, and the `unix://` dialing
  branch in `client.go`.
- `internal/auth/agentauth/middleware.go` — runner-side JWT middleware (the
  server now verifies via `apiauth`).
- The socket bind mount and `XAGENT_SERVER=unix://…` wiring in `runner.go`;
  the `AgentSocketPath` / `AgentSocketURL` constants.
- The runner's Ed25519 signing key: `cfg.PrivateKey`, `--private-key` /
  `XAGENT_PRIVATE_KEY`, and `agentauth.CreatePrivateKey`/`SignToken` usage in
  the runner. `agentauth` keeps `TaskClaims` + verify/sign, now exercised by the
  server with `appKey`.

## Migration plan

The cutover is incremental and keeps the proxy working until the direct path is
proven.

1. **Claims + server minting (no behavior change).** Add `org_id` and `exp` to
   `TaskClaims`; add `CreateTaskToken` RPC signing with `appKey`; add
   `AuthTypeTask` recognition in `apiauth` and the `TaskScopeInterceptor`
   (default-deny allowlist + ported filter checks). At this stage nothing routes
   through it yet.
2. **Dual enforcement.** Have the runner obtain the task token from
   `CreateTaskToken` instead of signing locally, but **keep routing through the
   proxy**. The proxy's `AgentFilter` still runs; the server's interceptor also
   runs. Both must agree — this validates parity in production with the proxy as
   a safety net. (The proxy verifies the token with `appKey`'s public half
   instead of the runner key.)
3. **Flip to direct in staging.** Change the injected `XAGENT_SERVER` /
   `--server` to the public URL for one workspace; confirm the driver and MCP
   tools work end-to-end and the interceptor enforces correctly. Roll to all
   workspaces.
4. **Delete.** Remove the proxy, `AgentFilter`, `UnixProxy`, the `unix://`
   branch, `agentauth.Middleware`, the bind mount, and the runner signing key.

Parity is testable directly: the existing `AgentFilter` tests become the
specification for `TaskScopeInterceptor`; run both against the same request
fixtures during step 2.

## Trade-offs

- **Server-minted vs. runner-signed tokens.** The alternative is to keep the
  runner signing and have it *register its public key* with the server so the
  server can verify. Rejected: it adds a key-registration/rotation surface, lets
  a compromised runner mint tokens for any task it can name, and keeps two key
  systems. Server-minting centralizes trust on the component that already owns
  org membership and already holds a signing key, and lets the server enforce
  `task.OrgID == caller.OrgID` at mint time.
- **Lineage in token vs. store lookup.** Embedding `parent` in the token avoids
  a store read per child operation but is stale-prone and can't see children
  created after minting. We chose authoritative store lookups; the reads are
  local and cheap, and they're strictly fewer than the proxy's current
  cross-process `GetTask` round-trips.
- **Interceptor vs. a second wrapped handler.** We could register a separate
  task-only Connect handler (mirroring how the proxy wraps a filter). An
  interceptor on the existing handler is less code, can't accidentally diverge
  from the real handlers, and naturally default-denies via the allowlist.
- **New egress requirement.** The proxy meant containers needed *zero* network
  reach to the server. Direct connection requires egress to one HTTPS endpoint.
  For the hosted product this is free; for locked-down self-hosters it's a real
  constraint, mitigated by `--agent-server-url` pointing at an internal address.

## Risks / open questions

- **Larger attack surface.** A task token is now a bearer credential usable from
  anywhere with network reach to the server, not just via a host-local socket. A
  leaked token grants exactly the task's scoped permissions within its org until
  `exp`. Mitigations: short-ish TTL, TLS in transit, default-deny allowlist, org
  scoping baked into the token, and server-side task-state checks (below).
- **Revocation.** JWTs are stateless. Because enforcement now runs in the server
  with the store, we can cheaply gate mutations on live task state — e.g. reject
  writes for an `archived`/terminal task (the filter already special-cases
  archived in `UpdateTask`). For hard revocation, add a per-task token epoch
  (a column bumped on cancel/rotate) included as a claim and checked server-side.
  Is implicit state-based revocation enough, or do we need an epoch/denylist
  from day one?
- **Blast radius.** Bounded to one task + its direct children, within one org,
  limited by workspace scopes — the same envelope `AgentFilter` enforces today.
  The new exposure is *reach*, not *scope*.
- **DoS / abuse.** A compromised container can now hit the server directly and
  repeatedly. The proxy implicitly rate-limited via the host. We should add
  per-task / per-org rate limiting and request-size limits to the task-caller
  path.
- **Token refresh semantics.** Should `CreateTaskToken` accept a task-token
  caller for self-refresh (simple, but a leaked token self-renews indefinitely),
  or should only the runner re-mint (requires the runner to track expiry and
  re-inject, but bounds a leaked token's life)? Leaning runner-driven re-mint on
  reload + a conservative TTL; needs a decision.
- **Self-hosted reachability.** Confirm the container→server address story for
  deployments where the runner reaches the server over an address containers
  can't (Docker network topology). `--agent-server-url` covers the common cases;
  are there setups where no single address works and a lightweight TCP forwarder
  (not an authenticating proxy) is still needed?
