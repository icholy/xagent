# Direct driver–C2 connection: eliminate the runner socket proxy

Issue: https://github.com/icholy/xagent/issues/890

Builds on: [`proposals/draft/scope-based-permissions.md`](./scope-based-permissions.md)
(the `authscope` engine and API-caller enforcement landed in #902/#905/#906).

## Problem

In-container agents never talk to the C2 server directly. The runner stands up a
Unix-socket proxy at `/xagent/socket` inside every container
(`internal/runner/proxy.go`, `internal/xagentclient/unix.go`) and bind-mounts its
parent directory into the container (`internal/runner/runner.go`). The driver and
the injected `xagent` MCP server dial that socket; the runner terminates the
connection, authorizes it, and forwards upstream using its own org-scoped `xat_`
key.

All authorization lives in the runner:

- `agentauth.Middleware` (`internal/auth/agentauth/middleware.go`) verifies a
  per-task JWT signed with the **runner's** Ed25519 key — a key the **server never
  sees**.
- `agentmcp.AgentFilter` (`internal/agentmcp/filter.go`) implements
  `XAgentServiceHandler` and gates every RPC with `scopes.Allow(...)`, restricting a
  container to its own task or, when the workspace enables it, its direct children.

This man-in-the-middle proxy has real costs (verbatim from the issue): container
connectivity is coupled to the runner process and a host bind mount; trust is
duplicated (the runner re-implements org scoping via its key plus task scoping via
`AgentFilter`); there are **two key systems** (runner-signed task JWTs vs.
server-signed app JWTs), so the server cannot independently identify or authorize a
task; and an agent can only reach the server through a co-located runner.

The scope work this proposal depends on just merged: every `apiserver` handler now
performs a coarse, op-level `Scopes.Allow` check (#906), `internal/auth/authscope`
is the shared engine, and `agentauth.Scopes(ScopeOptions{...})` already mints the
narrow per-task scope set that `AgentFilter` enforces today. The missing piece is a
token the **server itself** can verify and the per-instance attribute checks #906
deliberately deferred. This proposal supplies both.

## Design

The driver and the injected `xagent` MCP server connect **directly to the C2 over
the network**, presenting a **server-minted, server-signed task token**. The server
verifies the token, derives the calling task's identity from it, and enforces the
same per-task / parent-child / scope-gated permissions `AgentFilter` enforces today
— now in-process, as the first real consumer of non-admin scopes.

Four pieces, each grounded below:

1. A runner-authenticated **`CreateTaskToken`** RPC that mints the narrow token
   (issuance moves from the runner's key to the server's).
2. A **server-signed task token** verified as a third credential type in `apiauth`,
   replacing `agentauth.Middleware`.
3. **Validity gated on task state** (no expiry) — revocation for free.
4. **Per-task enforcement in the `apiserver` handlers**, absorbing `AgentFilter`.

### 1. `CreateTaskToken` — dedicated issuance RPC

Today the runner mints the token itself in `AgentProxy.TaskToken`
(`internal/runner/proxy.go`), signing `TaskClaims` with its own key. Instead, add a
purpose-built RPC the runner calls to have the **server** mint and sign the token:

```proto
// proto/xagent/v1/xagent.proto, alongside the other runner-facing RPCs
// (RegisterWorkspaces, ListRunnerTasks, SubmitRunnerEvents)
rpc CreateTaskToken(CreateTaskTokenRequest) returns (CreateTaskTokenResponse);

message CreateTaskTokenRequest {
  int64 task_id = 1;
  // Workspace capability flags the runner enabled for this task
  // (agentauth.CapabilityChildTasks, agentauth.CapabilityGitHubToken).
  repeated string capabilities = 2;
}

message CreateTaskTokenResponse {
  string token = 1;
}
```

This is deliberately **not** a generic "downscope my token" endpoint. Driver tokens
do not expire (§3), so a generic reducer that hands back a long-lived narrower token
is too dangerous. `CreateTaskToken` is purpose-built: the runner supplies only
`task_id` + the capability flags; the **server** loads the task row and derives
`workspace`/`runner` from it, then mints the scopes via the **existing** minter:

```go
// apiserver handler sketch — the runner cannot choose the scopes
func (s *Server) CreateTaskToken(ctx context.Context, req *xagentv1.CreateTaskTokenRequest) (*xagentv1.CreateTaskTokenResponse, error) {
    caller := apiauth.MustCaller(ctx)
    if !caller.Scopes.Allow(authscope.OpTaskTokenCreate) { // see §7
        return nil, errPermissionDenied("cannot mint task tokens")
    }
    // Tenancy: the task must belong to the caller's org (existing pattern).
    task, err := s.store.GetTask(ctx, nil, req.TaskId, caller.OrgID)
    if err != nil {
        return nil, err // NotFound for another org's task
    }
    scopes := agentauth.Scopes(agentauth.ScopeOptions{
        TaskID:       task.ID,
        Workspace:    task.Workspace, // derived from the row, NOT the request
        Runner:       task.Runner,    // derived from the row, NOT the request
        Capabilities: capabilities(req.Capabilities), // validated via agentauth.ValidCapability
    })
    token, err := s.taskTokens.Sign(&apiauth.TaskTokenClaims{TaskID: task.ID, Scopes: scopes})
    // ...
}
```

Deriving `workspace`/`runner` from the task row (not the request) is what makes the
conjunction in the `task.create` scope trustworthy: the runner cannot widen its
agents' sandbox by lying about workspace or runner, because it never supplies them.
`agentauth.Scopes` already fully constrains the create scope (parent + workspace +
runner), so completeness is the minter's responsibility exactly as today.

### 2. The task token: server-signed, server-verified

`AgentProxy.TaskToken` signs `agentauth.TaskClaims` with the runner's key; the proxy
verifies it with the same key. We replace this with a **server-held key**.

A new claims type lives in `apiauth` (one key system; verifiable by the server):

```go
// internal/auth/apiauth/tasktoken.go
type TaskTokenClaims struct {
    jwt.RegisteredClaims
    TokenType string           `json:"token_type"` // always "task" — the dispatch discriminator
    TaskID    int64            `json:"task_id"`
    Scopes    authscope.Scopes `json:"scopes,omitempty"`
}
```

Notably the token carries **no `org_id`, no `workspace`, no `runner`, and no `exp`**:
the org is derived from the task row at verification time (§4), and validity is
gated on task state (§3) rather than a clock.

**Signing key — recommendation: a dedicated server-held key, sibling to the app-JWT
key.** `apiauth.Auth` already owns `appKey` (`internal/auth/apiauth/apiauth.go`) for
app JWTs. Add a second server-owned Ed25519 key, `taskKey`, configured the same way
(hex seed via env, generated on startup if unset). Both are *the server's* keys —
"one key system" in the sense that matters (the server signs and verifies, unlike
the runner-signed JWT today) — but a distinct key keeps task-token verification and
app-JWT verification from ever being confused and lets the two rotate
independently. The cheaper alternative (reuse `appKey`, distinguish purely by the
`token_type` claim) is viable and noted in Trade-offs; the dispatch discriminator is
needed either way.

### 3. No expiry; validity gated on task state

A driver token effectively cannot expire: tasks run arbitrarily long and there is no
proxy to refresh a short-lived token. So instead of a clock, **the server treats a
task token as valid only while its task is in an active state**, checked on every
request during verification:

```go
// during apiauth verification of a task token
if task.Archived || task.IsDone() { // model.Task.IsDone() == completed/failed/cancelled
    return nil, errInvalidToken // 401
}
```

A task is active while `Status ∈ {Pending, Running, Restarting, Cancelling}` and not
`Archived`. This gives three things at once:

- **Revocation for free.** Archiving or cancelling a task immediately invalidates
  every token minted for it — no token blocklist, no expiry window.
- **Closes the "leaked archived-task token un-archives itself" hole.** Because an
  archived task fails the gate *before* any handler runs, a leaked token cannot call
  `UnarchiveTask` (or anything else) to resurrect itself via `task.write`.
- **Bounds the blast radius of the no-expiry tradeoff** to a task's active lifetime.

Restart interaction: `IsDone()` is non-absorbing (a completed task can be
restarted), but restart already kills the container and the runner mints a **fresh**
token for the new run, so gating on `IsDone()` never strands a live agent. The one
accepted edge is an agent making an API call *after* it has driven its own task to
`completed`; the driver reports completion as its final act, so this window is
empty in practice.

Cost: one task-row read per agent request during auth. This is the same row most
agent RPCs load anyway, and can share a request-scoped cache; it is the price of
state-gated validity and is acceptable.

### 4. Server-side verification path: a third credential in `apiauth`

`apiauth.authenticate` (`internal/auth/apiauth/apiauth.go`) already dispatches Bearer
tokens by shape: `xat_` prefix → `ValidateKey`; otherwise → `VerifyAppToken`. Task
tokens become the **third** credential, slotting in alongside `xat_` keys and app
JWTs and **replacing `agentauth.Middleware`** entirely:

```go
func (a *Auth) authenticate(r *http.Request) (*UserInfo, error) {
    raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
    if !ok {
        return nil, nil
    }
    if IsKey(raw) {
        // ... unchanged xat_ path ...
    }
    // Verify against the server's task key first; a task token carries
    // token_type=="task". Fall through to the app-JWT path otherwise.
    if claims, err := VerifyTaskToken(a.taskKey, raw); err == nil && claims.TokenType == "task" {
        task, err := a.tasks.GetActiveTask(r.Context(), claims.TaskID) // §3 state gate + org lookup
        if err != nil {
            return nil, err // archived/done/missing -> 401
        }
        return &UserInfo{
            OrgID:  task.OrgID,        // tenancy derived from the row
            Type:   AuthTypeTask,      // new constant
            Scopes: claims.Scopes,     // the narrow minted scopes
        }, nil
    }
    claims, err := VerifyAppToken(a.appKey, raw)
    // ... unchanged app path ...
}
```

The resulting `UserInfo` is an ordinary caller: `OrgID` from the task row, `Scopes`
from the token. Every existing org-scoped store call (`…(ctx, nil, …, caller.OrgID)`)
and every scope check then works **unmodified** — the task caller is just a caller
that happens to hold a very narrow scope set. `agentauth.Middleware` is deleted;
this verification path is its replacement, now in the server.

(`AuthTypeTask` also wants a small `AuditName()` branch so task callers are labelled
in audit logs, mirroring the existing `AuthTypeKey` case.)

### 5. Server-side per-task enforcement: the `apiserver` absorbs `AgentFilter`

#906 landed the API-caller scope checks **coarse**: per the scope proposal §7, the
API surface authorizes request-only and deliberately drops the own-OR-child
`task.parent` resolution, on the reasoning that *"an API caller is not a task."* The
task token breaks that premise — it **is** a task hitting the API surface directly.
So the per-instance attribute checks `AgentFilter` performs now come back, **in the
`apiserver` handlers**, as the scope proposal anticipated ("this is where those
attributes come back, now that the agent's narrow token makes them testable").

Concretely, the per-RPC logic from `agentmcp/filter.go` folds into the corresponding
`apiserver` handlers. Two shapes, both already present in `AgentFilter`:

- **Request-only top-gate** (no row needed): `CreateTask` authorizes on
  `task.parent`/`task.workspace`/`task.runner` from the request; `ListChildTasks` on
  `task.parent=req.ParentId`; `CreateLink`/`UploadLogs`/`SubmitRunnerEvents` on
  `task.id` from the request. These match `AgentFilter` verbatim and are pure
  top-of-handler checks (scope proposal §8 style).
- **Load-then-check own-OR-child** (relationship resolved against the row):
  `GetTask`, `GetTaskDetails`, `UpdateTask`, and `ListLogs` load the task row (which
  they already do to build a response) and authorize against
  `WithTaskID(row.Id)` **and** `WithTaskParent(row.Parent)` — exactly
  `AgentFilter.GetTask`/`UpdateTask`/`ListLogs`. This is the own-OR-child
  disjunction (scope proposal §6b): the token holds both
  `task.read:{task.id:self}` and `task.read:{task.parent:self}`, and the row's
  attributes match one or the other.

**This does not change behavior for existing callers.** Users, `xat_` keys, app
JWTs and cookie sessions hold `authscope.Admin()` (`*.*`) or coarse `task.read`/
`task.write`, which match any instance regardless of attributes — so adding
`WithTaskID(...)`/`WithTaskParent(...)` to a handler's `Allow` is transparent to
them and only constrains the narrow task caller. The handlers that gain a row load
(`GetTask`, `UpdateTask`, `ListLogs`) already load that row to respond, so the
overhead is nil.

`UpdateTask` keeps its **archived state guard after** the scope check, exactly as
`AgentFilter.UpdateTask` does today — that is a state guard, not a scope check.

#### `CreateGitHubToken` — the one RPC the runner does *not* just forward

`CreateGitHubToken` is special: the server handler returns `Unimplemented`
(`apiserver/github.go`, post-#806), and the live path is the runner — today
`AgentFilter` forwards it upstream, and the [optional runner-local GitHub
App](./split-github-app.md) draft would have the runner mint the installation token
**locally**, resolving repo→installation before ever reaching the C2. That local
mint is the one place the proxy does more than authorize-and-forward, and removing
the socket removes its interception point.

This proposal does not resolve the GitHub-token backend (that is the split-github-app
proposal's job), but it must not strand it. Options, with a recommendation:

- **(Recommended) Keep a minimal in-container `CreateGitHubToken` endpoint hosted by
  the runner, reachable only for this one RPC**, while everything else goes direct.
  But that re-introduces a (tiny) socket — contradicting the goal. Prefer instead:
- **(Recommended) Have the driver call `CreateGitHubToken` directly on the C2**, and
  let the **server** own the GitHub-token backend (central app, or a future
  server-side per-org app). The runner-local app in split-github-app becomes a
  server-side concern. This keeps the "server is the sole authority" model whole.
- **Defer**: ship the proxy removal for every RPC *except* `CreateGitHubToken`,
  keeping a runner-local mint path until split-github-app lands a server-side
  backend. Cleanest sequencing but leaves a vestigial runner endpoint.

Recommendation: route `CreateGitHubToken` to the server (option 2) and treat the
runner-local GitHub App as a follow-up the split-github-app proposal owns. Flagged as
Open Question #2.

### 6. How the driver and MCP server obtain and present the token

The wiring already exists — only the values change. Today
`internal/runner/runner.go` injects, per container:

- the driver `Cmd`: `xagent driver --server <socket-url> --token <jwt>` plus
  `XAGENT_SERVER`, `XAGENT_TOKEN`, `XAGENT_TASK_ID` env;
- the `xagent` MCP server args: `xagent tool agent-mcp --server <socket-url>
  --token <jwt> ...`;
- a bind mount of the socket directory.

After this change:

- The runner calls `CreateTaskToken(task_id, ws.Capabilities)` on the C2 (replacing
  the local `r.proxy.TaskToken(task, ws.Capabilities)` call in `runner.create`) and
  injects the **returned** token as `--token` / `XAGENT_TOKEN`, unchanged in shape
  from the driver/MCP point of view.
- `--server` / `XAGENT_SERVER` becomes the **real C2 URL** instead of
  `xagentclient.AgentSocketURL`. `xagentclient.New` already speaks both `unix://`
  and `http(s)://`; dropping the `unix://` branch is the only client change.
- The **socket bind mount and the socket-existence preflight are deleted** from
  `runner.create`.

**Network reachability & TLS.** The container must now reach the C2 over the
network. The runner already knows the C2 URL (its own `--server` /
`XAGENT_SERVER`); it passes that same URL to the container. For the common
single-host dev setup the container needs that URL to resolve from inside its
network namespace (e.g. the compose service name, or `host.docker.internal`), which
is a workspace networking concern (`ws.Container.Networks`) rather than a protocol
one. Production already terminates TLS at `xagent.choly.ca`, so direct connections
are HTTPS by default and the token rides in `Authorization: Bearer` over TLS exactly
like every other caller — a strict improvement over the previously
un-encrypted local Unix socket. How to default the in-container URL when the
runner's own `--server` is loopback is Open Question #3.

### 7. What authorizes the runner to call `CreateTaskToken`

The runner authenticates to the C2 with its org-scoped `xat_` key
(`internal/command/runner.go`). It needs a capability to mint task tokens. Add a new
op to the taxonomy:

```go
OpTaskTokenCreate = []string{"task_token", "create"}
```

Options for constraining it:

- **(Recommended) A coarse `task_token.create` scope, gated by org tenancy.** Any
  caller holding `task_token.create` may mint a token **only** for a task in its own
  org (the `GetTask(ctx, nil, req.TaskId, caller.OrgID)` tenancy check in §1). The
  minted token is always **strictly narrower** than the runner's own key, so this
  grants no escalation: a runner that can already act org-wide can mint a token that
  can act on one task. Today every runner key holds `authscope.Admin()` (the #902
  backfill), so this passes immediately; a future narrowed runner key would carry
  `task_token.create` explicitly.
- **Bind to the runner id (defense in depth).** Additionally require
  `task.Runner == <runner>` so a runner can only mint for tasks routed to it. The
  snag: a `xat_` key is org-scoped and not bound to a runner id, so the server can't
  *cryptographically* tie the key to a runner today — it could only check a
  `runner_id` the request supplies, which is advisory, not a trust boundary. Worth
  doing once keys can carry a runner identity; deferred.

Recommendation: ship the org-tenancy gate (option 1) now; pursue runner-id binding
when key identities support it. This is Open Question #1.

## Migration / cutover

The proxy and the direct path cannot both authorize a given container, but they can
**coexist across containers** during rollout: the server-signed task token and the
runner-signed JWT are different credentials, and a container is wired for exactly one
of them at creation time. In-flight containers keep their existing wiring until they
restart.

**Phase 0 — server can mint and verify (no behavior change).** Land
`CreateTaskToken`, the `taskKey`, `TaskTokenClaims`, and the third-credential path in
`apiauth.authenticate`. Nothing calls `CreateTaskToken` yet; `agentauth.Middleware`
and the proxy still run. Pure addition.

**Phase 1 — fold `AgentFilter` into the `apiserver` (no behavior change).** Add the
per-instance `task.id`/own-OR-child checks (§5) to the `apiserver` handlers. Existing
callers hold `*.*`/coarse scopes, so every check still passes; the proxy's
`AgentFilter` remains the live enforcer for agents. This is the riskiest correctness
step, so it ships behind the frozen `filter_test.go` behavioral spec re-expressed
against the `apiserver` handlers (the same tests that survived the #902 conversion).

**Phase 2 — cut new containers over to the direct path.** `runner.create` calls
`CreateTaskToken` and injects the real C2 URL instead of the socket URL; the bind
mount is dropped for **new** containers. Existing containers keep their socket until
they are restarted. The proxy keeps running to serve them. Roll out to one runner,
verify agents reach the C2 directly, then fan out.

**Phase 3 — drain and remove.** Once no container references the socket (all tasks
restarted or completed since Phase 2), delete the proxy and its key. A runner can
report whether any of its managed containers still hold a socket mount to confirm
the drain.

Rollback at any phase is a runner-config flip: point new containers back at the
socket URL and re-enable the proxy; the server-side additions (Phases 0–1) are inert
when unused.

## What gets deleted

Once Phase 3 completes:

- `internal/runner/proxy.go` — the entire `AgentProxy` (socket lifecycle,
  `agentauth.Middleware` wiring, and `TaskToken` signing).
- `internal/xagentclient/unix.go` — `UnixProxy`.
- The `unix://` dial branch and `AgentSocketPath`/`AgentSocketURL` in
  `internal/xagentclient/client.go`.
- The socket bind mount and the socket-existence preflight in `runner.create`
  (`internal/runner/runner.go`), plus the `SocketPath` plumbing.
- `internal/agentmcp/filter.go` — `AgentFilter` (its logic now lives in the
  `apiserver` handlers; `filter_test.go` migrates to test those).
- `internal/auth/agentauth/middleware.go` — `agentauth.Middleware` and the
  runner-side `VerifyToken`/`SignToken` for task JWTs.
- **The runner's task-JWT signing key**: the `--private-key` /
  `XAGENT_PRIVATE_KEY` Ed25519 seed (`internal/command/runner.go`,
  `configfile`) and `agentauth.CreatePrivateKey`. The runner no longer signs
  anything — it authenticates with its `xat_` key alone.

**Retained from `agentauth`**: `ScopeOptions`/`Scopes` (the minter, now called by the
server in `CreateTaskToken`), the `Capability*` constants and `ValidCapability`, and
the `TaskClaims` *scope-shaping* logic. Only the runner-side **signing/verification**
half of `agentauth` and the proxy go away. `TaskTokenClaims` (server-signed) is the
new carrier; whether `TaskClaims` is renamed/moved into `apiauth` or kept as a thin
alias is an implementation detail.

## Net trust model

Before: **two** authorities. The runner owns task identity (signs the JWT with a key
the server never sees) and enforces task isolation out-of-process (`AgentFilter`),
while the server owns org tenancy. The server cannot identify or authorize a task on
its own.

After: **the server is the sole authority.** It mints the task token, signs it with
its own key, derives the task's org and identity from the authoritative task row,
gates validity on task state, and enforces every per-task / parent-child / capability
rule in its own handlers via `authscope`. The runner is reduced to an *orchestrator*
that authenticates with an ordinary org-scoped `xat_` key and asks the server to mint
tokens — it holds no signing key and makes no authorization decision. Task isolation
becomes a **server-enforced property** rather than a consequence of forcing agents
through a runner-side filter, and the narrow task token is the first real consumer of
non-admin scopes.

## Trade-offs

**A task row read per agent request.** State-gated validity (§3) costs one task-row
lookup at auth time. The alternative — short-lived tokens with refresh — is
impossible without the proxy to refresh them, which is exactly what we are removing.
The read is cacheable per request and is the row most handlers load anyway. Worth it:
it buys revocation and closes the un-archive hole with no new bookkeeping.

**Dedicated `taskKey` vs. reusing `appKey`.** A separate server key means a second
seed to configure and rotate, but keeps task-token verification from ever colliding
with app-JWT verification and allows independent rotation (task tokens are
long-lived; app JWTs are 5-minute). Reusing `appKey` with only the `token_type`
discriminator is simpler and still "one key system," at the cost of coupling the two
lifetimes. Recommend the dedicated key; both are server-owned, which is the property
the issue cares about.

**Direct network exposure.** Every container now opens a connection to the C2 rather
than to a local socket. That is the point (operational flexibility — agents can run
anywhere they can reach the C2), but it widens the network surface: the C2 must be
reachable from container networks, and a leaked task token is usable from anywhere
until its task goes terminal. Mitigations: TLS everywhere (already true in prod), the
token is strictly task-narrow, and state-gating bounds its lifetime. Net: the trust
model is *simpler and stronger* (one authority, server-verifiable identity) even as
the network surface grows.

**No token expiry.** Driver tokens never expire by a clock. We accept this because
(a) there is no refresh path without the proxy, and (b) state-gating is a tighter
bound than any fixed TTL for a long-running task — a 24h task needs a ≥24h TTL
anyway, whereas state-gating revokes the instant the task ends.

**`CreateGitHubToken` loses its runner interception point.** The one RPC where the
runner does real work (local installation-token minting, per split-github-app) must
move server-side or keep a vestigial runner endpoint. Resolving this is deferred to
split-github-app; this proposal recommends routing it to the server (§5, Open
Question #2).

## Open Questions

1. **Runner authorization to mint (§7).** Ship the coarse `task_token.create` +
   org-tenancy gate now, or hold for runner-id-bound keys? Recommendation: ship
   coarse now (the minted token is strictly narrower than the runner's key, so no
   escalation), add runner-id binding when key identities support it.

2. **`CreateGitHubToken` backend (§5).** Route directly to the server (and have the
   server own the GitHub-token backend, absorbing the runner-local app concept), or
   keep a minimal runner-local mint endpoint until split-github-app lands? This
   proposal recommends server-side and defers the mechanism to split-github-app.

3. **In-container C2 URL defaulting (§6).** When the runner's own `--server` is a
   loopback/compose address, what URL does the container get so it resolves from
   inside its network namespace? Options: reuse the runner's `--server` verbatim
   (works when both share a network), a separate `--agent-server` override, or derive
   from `ws.Container.Networks`. Needs a default that works for the compose dev setup
   and prod without per-workspace config.

4. **`taskKey` rotation.** Long-lived task tokens are signed by `taskKey`; rotating
   it invalidates every outstanding task token (every active agent must re-mint).
   Acceptable as an operational "restart your agents" event, or do we need overlapping
   key acceptance (verify against current + previous key) for zero-downtime rotation?

5. **Drain detection for Phase 3 (Migration).** How does the server/runner know no
   container still depends on the socket before deleting the proxy? Proposed: the
   runner enumerates its managed containers for the legacy bind mount and reports a
   clean drain; alternatively, gate removal on "all tasks created before the Phase 2
   cutover are terminal."
