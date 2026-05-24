# Split GitHub App into Webhook App and Runner Agent App

Issue: https://github.com/icholy/xagent/issues/684

Supersedes parts of: [`proposals/accepted/github-app-installation-tokens.md`](../accepted/github-app-installation-tokens.md)

## Problem

The accepted [GitHub App Installation Token API](../accepted/github-app-installation-tokens.md) proposal funnels both webhook delivery and agent write operations through a single central xagent GitHub App. To install that app, a user has to grant the central xagent deployment full read/write access (Contents, Pull requests, etc.) on whatever repos the agents will touch.

In practice this is the single biggest install-time objection: users want the event routing (PR comments, issue updates flowing back to tasks) but they don't want a shared multi-tenant app holding write keys to their repos.

The accepted proposal correctly identified app installation tokens as better than personal OAuth tokens, but it left the broad-access concern on the table.

## Design

Split the single GitHub App into two:

| App | Identity | Permissions | Lives where | Used for |
| --- | --- | --- | --- | --- |
| **Webhook app** | Central xagent deployment | Low (metadata + read-only events) | Server | Webhooks, OAuth account linking, event routing |
| **Agent app** | Operator-owned | Write (Contents, Pull requests, Issues, …) | Runner | Minting installation tokens for git + agent API calls |

The webhook app stays a single shared app on a shared deployment; users keep trusting it because it can't write. The agent app is the operator's own GitHub App, registered under their own GitHub account/org, and installed on the operator's own repos. The broad-access install is still required, but it's against an app the operator controls — that's the trust unlock.

### Co-installation requirement

Both apps must be installed on the same repos. An event delivered to the webhook app only leads anywhere if the agent app can act on the same repo. The setup docs and the `/ui/settings` page must state this explicitly. If only the webhook app is installed, events still arrive and tasks can still be created — they just fail at the first git operation. If only the agent app is installed, agents can act but no events flow in. Detection of partial install state is left as an open question (see below).

### What stays from the accepted proposal

The webhook app reuses the existing central-app machinery in `internal/server/githubserver/` — narrowed in purpose but unchanged in shape. The following are **kept as-is**, now serving only the webhook app's identity:

- [`githubserver.Config`](../../internal/server/githubserver/githubserver.go) (`AppID`, `AppSlug`, `ClientID`, `ClientSecret`, `WebhookSecret`, `PrivateKey`) and the server flags `--github-app-id`, `--github-app-slug`, `--github-client-id`, `--github-client-secret`, `--github-webhook-secret`, `--github-private-key` (env `XAGENT_GITHUB_APP_PRIVATE_KEY`) defined in `internal/command/server.go`.
- `(*githubserver.Server).WebhookHandler()` and the `eventrouter.Router` that turns webhook deliveries into `events` rows.
- `(*githubserver.Server).OAuthLink()` (the `read:user` OAuth flow that backs `/github/login`).
- `LinkGitHubInstallation` RPC and `(*apiserver.Server).LinkGitHubInstallation` in `internal/server/apiserver/github.go`.
- React route `webui/src/routes/github.setup.tsx` (`/ui/github/setup`), the setup-url landing page that captures `installation_id` and calls `LinkGitHubInstallation`.
- `orgs.github_installation_id` column, migration `20260517000001_github_installation.sql`, `Store.SetOrgGitHubInstallation` and `Store.ClearGitHubInstallation`.
- `InstallationEvent` `action: "deleted"` handler that clears the installation when the webhook app is uninstalled.

Why kept: the webhook app needs an identity per org (for event routing), it needs the setup-page handoff to map an `installation_id` to an xagent org, and it needs to react when the install is removed. Nothing about that changes when write permissions move out of this app.

### What moves to the runner

The accepted proposal generated installation tokens on the **server**: it parsed the GitHub App private key into a `*ghinstallation.AppsTransport` at `githubserver.New`, and `(*apiserver.Server).CreateGitHubToken` resolved the caller's org's installation ID and called `(*githubserver.Server).CreateInstallationToken`.

In this proposal, token generation moves to the **runner**. The runner holds the agent app's private key. The server-side `CreateGitHubToken` RPC is removed (see "Server changes" below).

#### Runner config

Add three runner flags, matching the existing `--github-private-key` / `XAGENT_GITHUB_PRIVATE_KEY` pattern from the accepted proposal (but on the runner side):

```
--github-app-id           Agent GitHub App ID                XAGENT_GITHUB_APP_ID
--github-private-key      Agent GitHub App private key (PEM) XAGENT_GITHUB_APP_PRIVATE_KEY
--github-installation-id  Default installation ID (optional) XAGENT_GITHUB_INSTALLATION_ID
```

Wired into `internal/command/runner.go` alongside the existing `--server`, `--workspaces`, `--key`, `--private-key` flags. The private-key value accepts either the PEM content directly or a path (detected by `-----BEGIN` prefix), reusing `githubx.ParsePrivateKey`.

`--github-installation-id` is optional and used as the fallback when repo→installation resolution (below) returns nothing — typically for single-repo runners where it isn't worth listing installations. A runner that omits it must rely on resolution.

#### Repo→installation resolution

A single operator-owned GitHub App can be installed on multiple repos/orgs (typical case: the operator has it installed under their personal account and under a couple of team orgs). When the runner needs to mint a token, it needs to pick the right installation for the repo the agent is about to clone or push to.

Add a `githubapp` package on the runner side with:

```go
package githubapp

type Resolver struct {
    transport *ghinstallation.AppsTransport // signs JWTs as the agent app
    defaultID int64                         // from --github-installation-id, may be 0
    cache     installationCache             // repo → installationID, owner → installationID
}

func (r *Resolver) ForRepo(ctx context.Context, owner, repo string) (int64, error)
```

`ForRepo` first checks the cache, then calls `GET /repos/{owner}/{repo}/installation` (using the app JWT) to discover the installation ID for that specific repo. The result is cached per `owner/repo`. If `GET /repos/.../installation` returns 404 (app not installed on this repo), the resolver falls back to `defaultID`. If `defaultID` is also zero, the call fails with a clear "agent GitHub App is not installed on owner/repo" error that surfaces to the agent.

The runner can populate the cache eagerly at startup by calling `GET /app/installations` and, for each installation, `GET /installation/repositories`, but lazy resolution is sufficient for v1 — eager warm-up is an optimization.

#### Token endpoint over the existing socket proxy

The agent-facing interface does not change. Agents still call a `CreateGitHubToken` RPC over the Unix socket at `/xagent.sock`. The change is **who answers that call**.

`internal/runner/proxy.go` currently registers an `AgentFilter` (`internal/agentmcp/filter.go`) as the `xagentv1connect.XAgentServiceHandler` behind the socket. `AgentFilter` embeds `UnimplementedXAgentServiceHandler` and forwards every method to `p.client` (an `xagentclient.Client` pointed at the C2 server). `CreateGitHubToken` is currently one of those forwarded methods (`filter.go:192`).

Change: in `proxy.go`, inject a `LocalGitHubTokens` handler into `AgentFilter`. The filter still does scope enforcement (`claims.HasScope(agentauth.ScopeGitHubToken)`), but instead of calling `p.client.CreateGitHubToken`, it calls the local handler:

```go
// internal/agentmcp/filter.go
type AgentFilter struct {
    xagentv1connect.UnimplementedXAgentServiceHandler
    client      xagentv1connect.XAgentServiceClient
    githubToken GitHubTokenMinter // nil if the runner has no agent app configured
}

type GitHubTokenMinter interface {
    CreateGitHubToken(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error)
}

func (p *AgentFilter) CreateGitHubToken(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
    claims, err := p.claims(ctx)
    if err != nil {
        return nil, err
    }
    if !claims.HasScope(agentauth.ScopeGitHubToken) {
        return nil, errPermissionDenied("github token issuance is disabled for this workspace")
    }
    if p.githubToken == nil {
        return nil, connect.NewError(connect.CodeFailedPrecondition,
            errors.New("agent GitHub App is not configured on this runner"))
    }
    return p.githubToken.CreateGitHubToken(ctx, req)
}
```

The runner constructs and injects the minter in `proxy.go`:

```go
// pseudo-wiring inside runner.New / NewProxy
filter := agentmcp.NewAgentFilter(p.client)
if appCfg != nil {
    filter.GitHubTokens = githubapp.NewMinter(appCfg) // wraps the Resolver + ghinstallation
}
```

If the runner has no agent app configured, the filter returns `FailedPrecondition` — the same error shape the accepted proposal used on the server. Workspaces that don't have `scopes: ["github_token"]` still get `PermissionDenied` from the scope check, unchanged.

#### Per-token scoping

Because the runner now knows which task is calling (via `claims.TaskID`, `claims.Workspace`) **and** can correlate that to the workspace's clone URL(s), it can mint narrowly-scoped tokens. The GitHub API endpoint `POST /app/installations/{id}/access_tokens` accepts optional `repository_ids` and `permissions` body params; `ghinstallation` exposes these via `ghinstallation.NewFromAppsTransportWithOptions(... ghinstallation.WithInstallationTokenOptions(&github.InstallationTokenOptions{...}))`.

Extend `CreateGitHubTokenRequest` to carry an optional repository list, defaulting to the repos extracted from the workspace's `commands` (the `git clone …` lines):

```protobuf
message CreateGitHubTokenRequest {
  // Optional repo list (owner/name). Empty = all repos accessible to the
  // installation. The runner intersects with what it can derive from the
  // task's workspace clone URLs.
  repeated string repositories = 1;
}
```

The runner resolves these to numeric repo IDs (cached) and passes them through to `InstallationTokenOptions`. Default permissions stay at the installation's full permissions; tightening per-call is left as an open question.

This is something the server couldn't easily do in the accepted design, because the server didn't have visibility into which workspace/repo the calling task belonged to (only the org). The runner does.

#### Where the GitHub MCP and git-credential adapters live

Unchanged in shape — both still call `CreateGitHubToken` over the socket:

- `xagent tool git-credential` (`internal/command/git_credential.go`, package `gitcredential`) — git credential helper, called from `git config credential.https://github.com.helper`.
- `xagent tool github-mcp` (`internal/command/github_mcp.go`, package `githubmcp`) — stdio MCP adapter that hot-swaps the upstream session via `internal/mcpswap`.
- The `get_github_token` MCP tool in `internal/agentmcp/xmcp.go` (gated by `agentauth.ScopeGitHubToken`).

All three already use the in-container `XAGENT_SERVER` + `XAGENT_TOKEN` env vars to reach the socket. They don't know — and don't need to know — that the runner is now answering `CreateGitHubToken` locally instead of forwarding to the central server. Zero changes to these files.

### Server changes

- `(*apiserver.Server).CreateGitHubToken` (`internal/server/apiserver/github.go:71`) is removed.
- The `CreateGitHubToken` RPC method on the `XAgentService` proto stays defined (the runner-side filter still implements it; over-the-socket clients still call it). What's removed is the server's binding for it: in `apiserver`, return `connect.CodeUnimplemented` so any direct server call fails fast. Agents never reach this code path because their requests terminate at the runner's socket-proxy handler.
- `(*githubserver.Server).CreateInstallationToken` is removed. The webhook app no longer needs to mint installation tokens, so the `ghinstallation.AppsTransport` field in `githubserver.Server` is removed too. `githubserver.New` no longer parses a private key (the field stays in `Config` for backward compatibility with deployments still on the old binary, but is unused and can be deprecated in a follow-up).
- The webhook app's GitHub App permissions are reduced in the GitHub UI to metadata + the event subscriptions it needs (Pull requests events, Issues events, Issue comments events, Installation events). This is a one-time operator action, not a code change.

### Delta table

| Component | Accepted proposal | This proposal |
| --- | --- | --- |
| Webhook delivery / event routing | Server (central app) | Server (webhook app) — unchanged |
| OAuth account linking (`/github/login`) | Server | Server — unchanged |
| `/ui/github/setup` page + `LinkGitHubInstallation` RPC | Server | Server — unchanged |
| `orgs.github_installation_id` + migration | Server | Server — unchanged |
| `InstallationEvent` `deleted` handler | Server | Server — unchanged |
| GitHub App private key | Server (`XAGENT_GITHUB_APP_PRIVATE_KEY`) | Runner (`XAGENT_GITHUB_APP_PRIVATE_KEY`). Webhook app needs no private key. |
| App ID | Server (`--github-app-id`) | Both (webhook app id on server, agent app id on runner via `--github-app-id` / `XAGENT_GITHUB_APP_ID`) |
| Installation discovery | Setup page → `orgs.github_installation_id` | Runner repo→installation resolver (`GET /repos/{owner}/{repo}/installation`) with optional `--github-installation-id` fallback |
| `CreateGitHubToken` RPC handler | `(*apiserver.Server).CreateGitHubToken` (server) | Local handler on runner injected into `AgentFilter` |
| Token-minting library call | `(*githubserver.Server).CreateInstallationToken` | New `githubapp.Minter` on runner using `ghinstallation` |
| Per-token repo scoping | None | Repos derived from workspace clone URLs, passed via `InstallationTokenOptions` |
| `xagent tool git-credential` | Calls socket `CreateGitHubToken` (server-backed) | Calls socket `CreateGitHubToken` (runner-backed). No code change. |
| `xagent tool github-mcp` | Calls socket `CreateGitHubToken` (server-backed) | Calls socket `CreateGitHubToken` (runner-backed). No code change. |
| `get_github_token` MCP tool | Calls socket `CreateGitHubToken` (server-backed) | Calls socket `CreateGitHubToken` (runner-backed). No code change. |
| `agentauth.ScopeGitHubToken` workspace scope | Required | Required — unchanged |
| `gitbundler` external credential injection | Out of scope | Out of scope — but now naturally moves toward the agent app since gitbundler also runs near the runner |

## Trade-offs

**Two apps, two installs** — Operators have to create and maintain a second GitHub App (the agent app) and convince repo owners to install both. That's real setup friction. The mitigation: the webhook app stays the single shared central app (just create/install it once for the deployment), and only the agent app needs to be operator-specific. Most operators run xagent against a small set of their own repos, so registering one personal app and installing it on those repos is a one-time step.

**Operator now holds an app private key** — Compared to the accepted proposal's "server holds the key, runner never sees it" property, this proposal pushes the agent app's private key to the runner. The argument the accepted proposal made for centralization ("the runner already communicates with the server, so this adds no new trust boundary") cuts the other way here: in the multi-tenant central-deployment model, you want the key off the multi-tenant box. The runner is single-tenant (the operator owns it), so the key sitting there is no worse than the operator's existing personal SSH key or GitHub PAT.

**Token issuance latency is unchanged** — The runner already round-trips to GitHub for `ghinstallation`'s token cache; moving the cache from server to runner is a wash. The credential helper's per-git-call overhead is unchanged.

**Centralized events preserved** — Event routing keeps its current shape: one webhook URL per deployment, one `installation_id` per org, all events landing in the existing `events` table and dispatched via `eventrouter.Router`. The split happens at the *write path*, not the event path.

**Multi-runner / multi-installation** — Adding repo→installation resolution at the runner is strictly more flexible than the server's one-installation-per-org model. A single runner can serve repos from multiple GitHub orgs (each with its own install of the agent app) without any org-level table changes. The cost is one extra `GET /repos/{owner}/{repo}/installation` per uncached repo.

**No backward compatibility shim** — The server's `CreateGitHubToken` is deleted, not deprecated. The accepted proposal hasn't shipped any persistent state that depends on the server-side token path (only the install mapping, which is still used), so there's nothing to migrate; old runners pointed at a new server will just fail their `CreateGitHubToken` calls with `Unimplemented` and surface a clear error.

**Per-token scoping is a unique win of this design** — The server in the accepted proposal couldn't easily scope tokens because it didn't know which workspace/repo the calling task belonged to. Moving issuance to the runner makes scoping cheap because the runner already has the workspace config in memory.

## Open Questions

1. **Detecting partial install state.** If the agent app isn't installed on the repo the webhook event came from, the agent will fail at first git op. Should the server detect this at event delivery time (cross-check via the webhook app's `GET /installation/repositories` and warn) or leave it as a runtime failure with a clear error? Leaning runtime-only for v1.
2. **Default permission tightening.** `InstallationTokenOptions` accepts a per-token `permissions` map. We could default to `contents: write, pull_requests: write` and let workspace config widen. Or pass the installation's full permissions by default (current behavior) and let workspace config narrow. Needs a survey of what agents actually use.
3. **Webhook app's `PrivateKey` field.** Removed in spirit but the field stays in `githubserver.Config` for one release to ease deployment rollouts. Confirm or commit to a hard removal.
4. **`gitbundler` migration.** The accepted proposal called gitbundler out of scope. With the agent app now living near the runner, the natural fix is to have gitbundler also consume the runner's `githubapp.Minter`. Same scope decision: out of this proposal, but flagged.
