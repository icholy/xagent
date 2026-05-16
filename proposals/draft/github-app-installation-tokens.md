# GitHub App Installation Token API

Issue: https://github.com/icholy/xagent/issues/542

## Problem

The runner currently relies on personal GitHub credentials injected via workspace environment variables (e.g. `${sh:gh auth token}` or `${env:GITHUB_TOKEN}`) for git operations and GitHub API calls inside containers. This requires maintaining a separate GitHub user account and produces long-lived tokens with broad scopes.

We already have a GitHub App configured (`githubserver.Config` has `AppID` and `AppSlug`) for OAuth account linking and webhooks. GitHub Apps can generate short-lived installation access tokens (valid for 1 hour) scoped to the repositories the app is installed on. The server should expose an API to generate these tokens so the runner and agents can use them instead of personal credentials.

## Design

### Server-Side: GitHub App JWT Authentication

Add a `PrivateKey` field to `githubserver.Config` to hold the GitHub App's PEM-encoded private key. This is the private key downloaded when creating the GitHub App — it's used to sign JWTs that authenticate as the app itself.

```go
// internal/server/githubserver/githubserver.go
type Config struct {
    AppID         string
    AppSlug       string
    ClientID      string
    ClientSecret  string
    WebhookSecret string
    PrivateKey    []byte // PEM-encoded GitHub App private key (new)
}
```

New server flag and env var:

```
--github-private-key    GitHub App private key (PEM)    XAGENT_GITHUB_PRIVATE_KEY
```

The value can be either the PEM content directly or a file path (detect by checking for `-----BEGIN`). This follows the same pattern GitHub Actions uses for `APP_PRIVATE_KEY`.

### New RPC: CreateGitHubToken

Add a new RPC to the `XAgentService`:

```protobuf
rpc CreateGitHubToken(CreateGitHubTokenRequest) returns (CreateGitHubTokenResponse);

message CreateGitHubTokenRequest {
  // Empty — installation ID is resolved from the caller's org.
}

message CreateGitHubTokenResponse {
  // Short-lived installation access token.
  string token = 1;
  // When the token expires.
  google.protobuf.Timestamp expires_at = 2;
}
```

The handler:

1. Resolves the GitHub App installation ID from the caller's org (stored via webhook — see below).
2. Uses the GitHub App private key to sign a JWT (using `golang-jwt` with RS256, which is already an indirect dependency via `go-github`).
3. Calls the GitHub API `POST /app/installations/{installation_id}/access_tokens` with the JWT.
4. Returns the installation token and its expiry.

Authentication: This endpoint requires a valid xagent API key or session (same as other RPCs). Tokens use the full installation permissions — no additional scoping for now.

### Implementation

Use the `github.com/bradleyfalzon/ghinstallation/v2` library for app authentication and token generation:

```go
// internal/server/apiserver/github.go
func (s *Server) CreateGitHubToken(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
    if s.githubAppKey == nil {
        return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("GitHub App private key not configured"))
    }
    caller := apiauth.Caller(ctx)
    org, err := s.store.GetOrg(ctx, nil, caller.OrgID)
    if err != nil {
        return nil, connect.NewError(connect.CodeInternal, err)
    }
    if org.GitHubInstallationID == 0 {
        return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("no GitHub App installation linked to this org"))
    }

    // Create JWT signed with app private key
    transport, err := ghinstallation.New(http.DefaultTransport, s.githubAppID, org.GitHubInstallationID, s.githubAppKey)
    if err != nil {
        return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create transport: %w", err))
    }
    token, err := transport.Token(ctx)
    if err != nil {
        return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create installation token: %w", err))
    }

    return &xagentv1.CreateGitHubTokenResponse{
        Token:     token,
        ExpiresAt: timestamppb.New(transport.Expiry),
    }, nil
}
```

### In-Container Git Credential Helper

Git operations inside the container — the initial clone, plus any later `git fetch`/`push` from the agent — need a token. Injecting `GITHUB_TOKEN` into the container environment goes stale after 1h, breaking any git call made after that.

Instead, add a `git-credential` subcommand to the xagent binary (already mounted in the container as the agent driver). The runner adds a `git config` line to the workspace's pre-clone setup commands, scoped to `github.com` so non-GitHub URLs fall through to git's default behavior:

```yaml
commands:
  - git config --global credential.https://github.com.helper "!xagent git-credential"
  - git clone --bundle-uri http://gitbundler/xagent.bundle https://github.com/icholy/xagent.git
```

The clone URL no longer contains an inline token.

The helper reads git's stdin (`protocol=https`, `host=github.com`, ...), short-circuits unless `host=github.com`, calls `CreateGitHubToken` over the Unix socket proxy, and writes:

```
username=x-access-token
password=<token>

```

`store`/`erase` actions are no-ops — the server is authoritative.

`ghinstallation` caches tokens server-side and re-fetches near expiry, so every git call gets a fresh-enough token. The 1h TTL is invisible to in-container code.

If `CreateGitHubToken` fails during the initial clone, the setup commands fail and the task aborts — same behavior as any pre-existing setup-command failure.

### Agent MCP Tool: `get_github_token`

Add a tool to the agent MCP server (`internal/agentmcp/xmcp.go`) for the rare case an agent needs a raw `GITHUB_TOKEN` for a tool that doesn't go through git or the github MCP server (e.g., a one-off `gh` invocation):

```go
type getGitHubTokenInput struct{}
```

This calls `CreateGitHubToken` on the server via the existing Unix socket proxy. Primary GitHub API access happens through the github MCP server (proxied via mcproxy — see below), and git uses the credential helper, so this tool is a fallback.

### Installation ID Discovery via Setup Page

When a user installs the GitHub App, GitHub redirects them to the app's **setup URL** with the `installation_id` as a query parameter. The setup URL points to a frontend page where the user can choose which xagent org to associate the installation with.

GitHub Apps support a `setup_url` configuration (set in the GitHub App settings). Configure it to point to the frontend:

```
Setup URL: https://xagent.example.com/ui/github/setup
```

#### Frontend Route

New React route at `webui/src/routes/github.setup.tsx` (maps to `/ui/github/setup`), following the same pattern as the MCP OAuth authorize page (`webui/src/routes/oauth.authorize.tsx`):

- Reads `installation_id` from the URL query params
- Shows the user's available orgs (from `getProfile`)
- Lets the user select which org to associate the installation with
- On confirm, calls a new `LinkGitHubInstallation` RPC with the installation ID and selected org ID
- Redirects to `/ui/settings` on success

```tsx
// webui/src/routes/github.setup.tsx
export const Route = createFileRoute('/github/setup')({
  component: GitHubSetupPage,
})

function GitHubSetupPage() {
  const installationId = new URLSearchParams(window.location.search).get('installation_id')
  const { data: profileData } = useQuery(getProfile, {})
  const orgs = profileData?.orgs ?? []
  // ... org selector + confirm button
}
```

#### Backend RPC

New RPC to store the installation mapping:

```protobuf
rpc LinkGitHubInstallation(LinkGitHubInstallationRequest) returns (LinkGitHubInstallationResponse);

message LinkGitHubInstallationRequest {
  int64 installation_id = 1;
}

message LinkGitHubInstallationResponse {}
```

The handler uses the caller's org from auth context:

```go
func (s *Server) LinkGitHubInstallation(ctx context.Context, req *xagentv1.LinkGitHubInstallationRequest) (*xagentv1.LinkGitHubInstallationResponse, error) {
    caller := apiauth.Caller(ctx)
    if err := s.store.SetOrgGitHubInstallation(ctx, nil, caller.OrgID, req.InstallationId); err != nil {
        return nil, connect.NewError(connect.CodeInternal, err)
    }
    return &xagentv1.LinkGitHubInstallationResponse{}, nil
}
```

#### Webhook Handler for Uninstalls

Handle `InstallationEvent` with `action: "deleted"` to clear the installation ID when the app is uninstalled:

```go
case *github.InstallationEvent:
    if event.GetAction() == "deleted" {
        installationID := event.GetInstallation().GetID()
        s.store.ClearGitHubInstallation(ctx, nil, installationID)
    }
```

#### Database Migration

New migration `internal/store/sql/migrations/NNN_github_installation.sql`:

```sql
ALTER TABLE orgs ADD COLUMN github_installation_id BIGINT;
```

#### Store Methods

```go
func (s *Store) SetOrgGitHubInstallation(ctx context.Context, tx *sql.Tx, orgID int64, installationID int64) error
func (s *Store) ClearGitHubInstallation(ctx context.Context, tx *sql.Tx, installationID int64) error
```

#### Token Request Flow

With the installation ID stored on the org, `CreateGitHubToken` no longer requires the caller to pass an installation ID. The server resolves it from the authenticated caller's org context:

```protobuf
message CreateGitHubTokenRequest {
  // Empty — installation ID resolved from the caller's org.
}
```

```go
func (s *Server) CreateGitHubToken(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
    caller := apiauth.Caller(ctx)
    org, err := s.store.GetOrg(ctx, nil, caller.OrgID)
    if err != nil {
        return nil, connect.NewError(connect.CodeInternal, err)
    }
    if org.GitHubInstallationID == 0 {
        return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("no GitHub App installation linked to this org"))
    }
    // ... generate token using org.GitHubInstallationID
}
```

### MCP Proxy Token Provider

The runner sits behind [mcproxy](https://github.com/icholy/mcproxy), which aggregates upstream MCP servers — including GitHub's MCP server at `https://api.githubcopilot.com/mcp/`. That upstream's `Authorization: Bearer <token>` header needs the same installation token, and also needs to refresh hourly.

mcproxy has pluggable credential providers (`${type:name}` template syntax) and a rotation `Bus` that triggers hot-swaps of upstream sessions when a credential changes. Add an `xagent` provider that:

1. On startup, calls `CreateGitHubToken` on the xagent server (authenticated via an xagent API key).
2. Tracks the returned `expires_at`.
3. Re-fetches before expiry and `Publish`es to the `Bus`, causing mcproxy to reopen the GitHub upstream session with the new token.

Proxy config sketch:

```json
{
  "providers": [
    {
      "type": "xagent",
      "endpoint": "https://xagent.example.com",
      "api_key": "${env:XAGENT_API_KEY}",
      "min_ttl": "5m"
    }
  ],
  "servers": {
    "github": {
      "transport": "streamable",
      "url": "https://api.githubcopilot.com/mcp/",
      "headers": {
        "Authorization": "Bearer ${xagent:github_token}"
      }
    }
  }
}
```

The provider implementation lives in the mcproxy repo. This proposal only commits xagent to exposing `CreateGitHubToken` under API-key auth, which it already does for any RPC.

## Trade-offs

**App JWT vs. OAuth tokens**: OAuth tokens (from the existing account linking flow) authenticate as a user and require `read:user` scope. App installation tokens authenticate as the app and have whatever permissions the app was granted during installation. Installation tokens are better because they don't require a user account and have explicit, auditable permissions.

**Token generation on server vs. runner**: Generating tokens on the server keeps the private key centralized — the runner never sees it. The runner already communicates with the server, so this adds no new trust boundary. If the runner generated tokens itself, the private key would need to be distributed to every runner.

**`ghinstallation` library**: Use `github.com/bradleyfalzon/ghinstallation/v2` for JWT signing, token caching, and the GitHub API call. It handles the complexity of app authentication and is widely used.

**Credential helper vs. env injection**: Injecting `GITHUB_TOKEN` at container start is simpler but the token goes stale after 1h, breaking any later git operation. A credential helper that hits the server on every git call avoids that — token refresh stays server-side via `ghinstallation`'s cache. The cost is a small extra binary surface (`xagent git-credential`) and one `git config` line in the setup commands.

**`gh` CLI and other env-reading tools**: With no env injection, anything that reads `GITHUB_TOKEN` from the environment loses auth. The agent's primary GitHub API path is the github MCP server (via mcproxy), so this is rarely a problem. For the occasional shell-out, the `get_github_token` MCP tool provides a raw token on demand.

**gitbundler**: The gitbundler service that produces clone bundles runs outside the container and still reads its credentials from `${env:...}` at startup. Migrating it to installation tokens is a separate change — out of scope here.

## Open Questions

None — all decisions resolved during review.
