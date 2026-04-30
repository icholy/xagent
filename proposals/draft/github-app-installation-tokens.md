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
  // GitHub App installation ID. Required.
  int64 installation_id = 1;
}

message CreateGitHubTokenResponse {
  // Short-lived installation access token.
  string token = 1;
  // When the token expires.
  google.protobuf.Timestamp expires_at = 2;
}
```

The handler:

1. Uses the GitHub App private key to sign a JWT (using `golang-jwt` with RS256, which is already an indirect dependency via `go-github`).
2. Calls the GitHub API `POST /app/installations/{installation_id}/access_tokens` with the JWT.
3. Returns the installation token and its expiry.

Authentication: This endpoint requires a valid xagent API key or session (same as other RPCs). The caller must know the installation ID.

### Implementation

Use the `github.com/bradleyfalzon/ghinstallation/v2` library (or implement directly — it's ~30 lines):

```go
// internal/server/apiserver/github.go
func (s *Server) CreateGitHubToken(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
    if s.githubAppKey == nil {
        return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("GitHub App private key not configured"))
    }
    if req.InstallationId == 0 {
        return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("installation_id is required"))
    }

    // Create JWT signed with app private key
    transport, err := ghinstallation.New(http.DefaultTransport, s.githubAppID, req.InstallationId, s.githubAppKey)
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

Add a new tool to the agent MCP server (`internal/agentmcp/xmcp.go`) so agents running inside containers can request tokens:

```go
type getGitHubTokenInput struct {
    InstallationID int64 `json:"installation_id" jsonschema:"GitHub App installation ID"`
}
```

This calls `CreateGitHubToken` on the server via the existing Unix socket proxy. The agent can then use the returned token for git operations and GitHub API calls.

### Workspace Configuration

With this API, workspaces no longer need to inject personal tokens. Instead, agents can call `get_github_token` to get credentials on demand. A workspace that currently does:

```yaml
commands:
  - git clone https://x-access-token:${sh:gh auth token}@github.com/org/repo.git
```

Would instead have the agent use the MCP tool to get a token, then configure git credentials. The workspace prompt can instruct the agent to call `get_github_token` before cloning.

Alternatively, the runner could call `CreateGitHubToken` itself and inject the token as an environment variable (`GITHUB_TOKEN`) before starting the container. This is simpler for the agent but means the token starts its 1-hour expiry clock at container creation time.

### Installation ID Discovery

The caller needs to know the installation ID. There are several options:

1. **Workspace config**: Add an `installation_id` field to the workspace definition. The admin sets this when configuring the workspace.
2. **Webhook capture**: When the GitHub App is installed, GitHub sends an `installation` webhook event. The server could store this mapping (org/repos → installation ID) and expose a lookup API.
3. **API lookup**: The server could expose a `ListGitHubInstallations` RPC that lists all installations for the app (using the app JWT), letting callers find the right one.

Option 1 is simplest and sufficient for an initial implementation. Options 2 and 3 could be added later.

## Trade-offs

**App JWT vs. OAuth tokens**: OAuth tokens (from the existing account linking flow) authenticate as a user and require `read:user` scope. App installation tokens authenticate as the app and have whatever permissions the app was granted during installation. Installation tokens are better because they don't require a user account and have explicit, auditable permissions.

**Token generation on server vs. runner**: Generating tokens on the server keeps the private key centralized — the runner never sees it. The runner already communicates with the server, so this adds no new trust boundary. If the runner generated tokens itself, the private key would need to be distributed to every runner.

**`ghinstallation` library vs. manual JWT**: The `ghinstallation` library handles JWT signing, token caching, and the API call. It's widely used and maintained. Implementing manually is ~30 lines but loses the caching. Either approach works.

**MCP tool vs. environment variable injection**: Exposing `get_github_token` as an MCP tool lets the agent request tokens on demand and handle token refresh for long-running tasks. Injecting via env var is simpler but the token may expire during long tasks. Both approaches could be supported.

## Open Questions

1. **Installation ID source**: Should the initial implementation require `installation_id` in the workspace config, or should the server store installation mappings from webhook events?
2. **Token scoping**: GitHub's installation token API supports narrowing permissions and restricting to specific repositories. Should `CreateGitHubToken` accept optional scope/repository filters, or always use the full installation permissions?
3. **Runner-side injection**: Should the runner automatically call `CreateGitHubToken` and inject `GITHUB_TOKEN` into the container environment, or should this be agent-driven via the MCP tool?
