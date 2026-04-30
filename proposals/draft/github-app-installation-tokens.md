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

### Agent MCP Tool: `get_github_token`

Add a new tool to the agent MCP server (`internal/agentmcp/xmcp.go`) so agents running inside containers can request fresh tokens when the injected `GITHUB_TOKEN` expires:

```go
type getGitHubTokenInput struct{}
```

This calls `CreateGitHubToken` on the server via the existing Unix socket proxy. The installation ID is resolved server-side from the org. The agent can use the returned token to update its git credentials or make GitHub API calls.

### Runner-Side Token Injection

The runner calls `CreateGitHubToken` before starting the container and injects the result as `GITHUB_TOKEN` in the container environment. This gives the agent a working token from the start — no workspace config changes needed for git clones or GitHub API calls.

In `internal/runner/runner.go`, after building the agent config and before creating the container:

```go
// Fetch GitHub installation token for the task's org
if r.githubEnabled {
    tokenResp, err := r.client.CreateGitHubToken(ctx, &xagentv1.CreateGitHubTokenRequest{})
    if err != nil {
        r.log.Warn("failed to get GitHub token", "error", err)
    } else {
        b.Env = append(b.Env, "GITHUB_TOKEN="+tokenResp.Token)
    }
}
```

The token starts its 1-hour expiry clock at container creation time. For long-running tasks, the agent can call `get_github_token` via the MCP tool to get a fresh token.

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

## Trade-offs

**App JWT vs. OAuth tokens**: OAuth tokens (from the existing account linking flow) authenticate as a user and require `read:user` scope. App installation tokens authenticate as the app and have whatever permissions the app was granted during installation. Installation tokens are better because they don't require a user account and have explicit, auditable permissions.

**Token generation on server vs. runner**: Generating tokens on the server keeps the private key centralized — the runner never sees it. The runner already communicates with the server, so this adds no new trust boundary. If the runner generated tokens itself, the private key would need to be distributed to every runner.

**`ghinstallation` library**: Use `github.com/bradleyfalzon/ghinstallation/v2` for JWT signing, token caching, and the GitHub API call. It handles the complexity of app authentication and is widely used.

**Both injection and MCP tool**: The runner injects `GITHUB_TOKEN` at container start for immediate use. The agent MCP tool `get_github_token` provides refresh capability for long-running tasks where the initial token (1-hour TTL) may expire.

## Open Questions

None — all decisions resolved during review.
