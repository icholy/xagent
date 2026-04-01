# Server-Side Jira Integration

Issue: https://github.com/icholy/xagent/issues/327

## Problem

xagent has a GitHub integration that lets users link their GitHub accounts and receive webhook events routed to tasks. There is no equivalent Jira integration. Users who track work in Jira cannot receive webhook-driven notifications on their tasks when Jira issues are commented on or updated.

## Design

This proposal mirrors the existing GitHub integration pattern: OAuth account linking on the user, webhook secret on the org, a webhook handler that routes events to tasks via notify links.

### 1. Database Migration

New migration `internal/store/sql/migrations/015_jira.sql`:

```sql
ALTER TABLE users ADD COLUMN atlassian_account_id TEXT;
CREATE UNIQUE INDEX idx_users_atlassian_account_id ON users(atlassian_account_id);

ALTER TABLE orgs ADD COLUMN jira_webhook_secret TEXT NOT NULL DEFAULT '';
```

**Rationale:** Same pattern as GitHub — the Atlassian account ID is stored directly on the `users` table (nullable, unique indexed) rather than in a separate table. The webhook secret is per-org because each org has its own Jira Cloud instance, unlike GitHub where a single GitHub App webhook secret is configured globally.

### 2. Store Layer

Add to `internal/store/sql/queries/user.sql`:

```sql
-- name: GetUserByAtlassianAccountID :one
SELECT id, email, name, github_user_id, github_username, atlassian_account_id, default_org_id, created_at, updated_at
FROM users
WHERE atlassian_account_id = $1;

-- name: LinkAtlassianAccount :exec
UPDATE users SET
    atlassian_account_id = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UnlinkAtlassianAccount :exec
UPDATE users SET
    atlassian_account_id = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1;
```

Add to `internal/store/sql/queries/org.sql`:

```sql
-- name: GetOrgJiraWebhookSecret :one
SELECT jira_webhook_secret FROM orgs WHERE id = $1;

-- name: SetOrgJiraWebhookSecret :exec
UPDATE orgs SET
    jira_webhook_secret = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1;
```

All existing user queries (`UpsertUser`, `CreateUser`, `GetUser`, `GetUserByEmail`, `GetUserByGitHubUserID`) must add `atlassian_account_id` to their SELECT lists.

### 3. Model Updates

Update `internal/model/user.go`:

```go
type User struct {
    ID                  string    `json:"id"`
    Email               string    `json:"email"`
    Name                string    `json:"name"`
    GitHubUserID        int64     `json:"github_user_id"`
    GitHubUsername      string    `json:"github_username"`
    AtlassianAccountID  string    `json:"atlassian_account_id"`
    DefaultOrgID        int64     `json:"default_org_id"`
    CreatedAt           time.Time `json:"created_at"`
    UpdatedAt           time.Time `json:"updated_at"`
}

func (u *User) HasAtlassian() bool {
    return u.AtlassianAccountID != ""
}

func (u *User) AtlassianAccountProto() *xagentv1.JiraAccount {
    if !u.HasAtlassian() {
        return nil
    }
    return &xagentv1.JiraAccount{
        AtlassianAccountId: u.AtlassianAccountID,
        CreatedAt:          timestamppb.New(u.CreatedAt),
    }
}
```

Update `internal/model/org.go`:

```go
type Org struct {
    ID               int64     `json:"id"`
    Name             string    `json:"name"`
    Owner            string    `json:"owner"`
    Archived         bool      `json:"archived"`
    JiraWebhookSecret string  `json:"jira_webhook_secret"`
    CreatedAt        time.Time `json:"created_at"`
    UpdatedAt        time.Time `json:"updated_at"`
}
```

### 4. Proto Definitions

Add to `proto/xagent/v1/xagent.proto`:

```protobuf
// In service XAgentService:
rpc GetJiraAccount(GetJiraAccountRequest) returns (GetJiraAccountResponse);
rpc UnlinkJiraAccount(UnlinkJiraAccountRequest) returns (UnlinkJiraAccountResponse);
rpc GetJiraWebhookSecret(GetJiraWebhookSecretRequest) returns (GetJiraWebhookSecretResponse);
rpc GenerateJiraWebhookSecret(GenerateJiraWebhookSecretRequest) returns (GenerateJiraWebhookSecretResponse);

// Messages:
message JiraAccount {
  string atlassian_account_id = 1;
  google.protobuf.Timestamp created_at = 2;
}

message GetJiraAccountRequest {}
message GetJiraAccountResponse {
  JiraAccount account = 1;
}

message UnlinkJiraAccountRequest {}
message UnlinkJiraAccountResponse {}

message GetJiraWebhookSecretRequest {}
message GetJiraWebhookSecretResponse {
  string secret = 1;
  string webhook_url = 2;
}

message GenerateJiraWebhookSecretRequest {}
message GenerateJiraWebhookSecretResponse {
  string secret = 1;
  string webhook_url = 2;
}
```

`GetJiraAccount` returns the current user's linked Atlassian account (mirrors `GetGitHubAccount`). `GetJiraWebhookSecret` / `GenerateJiraWebhookSecret` are org-scoped — they operate on the caller's current org.

### 5. Atlassian OAuth Flow

Create `internal/jiraauth/jiraauth.go` mirroring `internal/ghauth/ghauth.go`:

```go
package jiraauth

type Config struct {
    ClientID     string
    ClientSecret string
    RedirectURL  string
    Log          *slog.Logger
    OnSuccess    func(w http.ResponseWriter, r *http.Request, accountID string)
}

type Handler struct {
    oauth     *oauth2.Config
    log       *slog.Logger
    onSuccess func(w http.ResponseWriter, r *http.Request, accountID string)
    mux       *http.ServeMux
}
```

Key differences from `ghauth`:

- **OAuth endpoint:** `https://auth.atlassian.com/authorize` / `https://auth.atlassian.com/oauth/token` (Atlassian OAuth 2.0 3LO)
- **Scopes:** `read:me` (to fetch account ID)
- **User fetch:** After token exchange, call `GET https://api.atlassian.com/me` with Bearer token to get `account_id`
- **State cookie:** `xagent_jira_state` (same TTL and security flags as GitHub)
- **Routes:** `/login` and `/callback` (mounted at `/jira/`)
- **OnSuccess callback:** Passes `accountID string` instead of `*github.User`

The `/me` endpoint returns:

```json
{
  "account_id": "5a4b...",
  "email": "user@example.com",
  "name": "User Name",
  ...
}
```

### 6. Webhook Handler

Create `internal/webhook/jira.go` mirroring `internal/webhook/github.go`:

```go
type JiraHandler struct {
    Log   *slog.Logger
    Store *store.Store
}
```

**Key difference from `GitHubHandler`:** No `WebhookSecret` field on the handler. The Jira webhook secret is per-org, looked up from the database at request time using the `org` query parameter.

Endpoint: `POST /webhook/jira?org=<org_id>`

Processing steps:

1. Extract `org_id` from `org` query parameter
2. Look up the org's `jira_webhook_secret` from the database
3. Verify HMAC-SHA256 signature using `X-Hub-Signature` header (Jira Cloud uses the same `X-Hub-Signature` header format as GitHub)
4. Parse the webhook payload JSON
5. Extract event details (comment body, author account ID, issue URL)
6. Enforce `xagent:` prefix on comment body (same pattern as GitHub)
7. Look up user by `atlassian_account_id` using `GetUserByAtlassianAccountID`
8. If not found, ignore (user hasn't linked their Atlassian account)
9. Call `findLinksByOrg()` to find matching notify links (reuse the same pattern from `github.go`)
10. Create events and route to tasks per org using `routeEventToLinks()`

**Deduplication:** Use `X-Atlassian-Webhook-Identifier` header value. This can be stored as part of the event or used to skip duplicate deliveries.

**Supported Jira webhook events:**

- `comment_created` — A comment was added to an issue. Extract `comment.author.accountId`, comment body, and issue URL.
- `comment_updated` — A comment was updated. Same extraction as `comment_created`.

The `extractJiraWebhookEvent` function mirrors `extractGitHubWebhookEvent`:

```go
type jiraWebhookEvent struct {
    description          string
    data                 string
    url                  string
    atlassianAccountID   string
}
```

**Event routing:** Reuse the same `findLinksByOrg` and `routeEventToLinks` patterns. These can be extracted into shared helpers in the `webhook` package or duplicated (the logic is simple). The `findLinksByOrg` method calls `store.FindNotifyLinksByURLForUser` which is already provider-agnostic — it matches on link URL regardless of whether the link points to a GitHub or Jira resource.

### 7. Server RPC Handlers

Add to `internal/server/server.go`:

**GetJiraAccount:**
```go
func (s *Server) GetJiraAccount(ctx context.Context, req *xagentv1.GetJiraAccountRequest) (*xagentv1.GetJiraAccountResponse, error) {
    caller := apiauth.MustCaller(ctx)
    resp := &xagentv1.GetJiraAccountResponse{}
    user, err := s.store.GetUser(ctx, nil, caller.ID)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return resp, nil
        }
        return nil, connect.NewError(connect.CodeInternal, err)
    }
    resp.Account = user.AtlassianAccountProto()
    return resp, nil
}
```

**UnlinkJiraAccount:** Clear `atlassian_account_id` on the caller's user record.

**GetJiraWebhookSecret:** Return the current org's `jira_webhook_secret` and the webhook URL (`{baseURL}/webhook/jira?org={orgID}`). Uses `caller.OrgID` from context.

**GenerateJiraWebhookSecret:** Generate a cryptographically random secret (e.g. 32 bytes, hex-encoded), store it on the org via `SetOrgJiraWebhookSecret`, and return it along with the webhook URL. This is idempotent — calling it again replaces the previous secret.

### 8. Server HTTP Routes

Add to `server.Handler()`:

```go
// Jira OAuth routes (conditional on Jira config)
if s.jira != nil {
    jh := jiraauth.New(jiraauth.Config{
        ClientID:     s.jira.ClientID,
        ClientSecret: s.jira.ClientSecret,
        RedirectURL:  s.baseURL + "/jira/callback",
        Log:          s.log,
        OnSuccess: func(w http.ResponseWriter, r *http.Request, accountID string) {
            caller := apiauth.Caller(r.Context())
            if caller == nil {
                http.Error(w, "not authenticated", http.StatusUnauthorized)
                return
            }
            if err := s.store.LinkAtlassianAccount(r.Context(), nil, caller.ID, accountID); err != nil {
                http.Error(w, "failed to link Jira account", http.StatusInternalServerError)
                return
            }
            http.Redirect(w, r, "/ui/settings", http.StatusFound)
        },
    })
    mux.Handle("/jira/", alice.New(s.auth.RequireAuth(), s.auth.AttachUserInfo()).Then(http.StripPrefix("/jira", jh)))
}

// Jira webhook (always registered — uses per-org secrets from DB)
mux.Handle("/webhook/jira", &webhook.JiraHandler{
    Log:   s.log,
    Store: s.store,
})
```

**Important:** The webhook endpoint is **always registered** (not gated on Jira config) because webhook secrets are per-org in the database. Even without OAuth configured, orgs can still receive webhooks. The OAuth routes are conditional on having `--jira-client-id` / `--jira-client-secret` configured.

### 9. CLI Flags

Add to `internal/command/server.go`:

```go
&cli.StringFlag{
    Name:    "jira-client-id",
    Usage:   "Atlassian OAuth client ID (for Jira account linking)",
    Sources: cli.EnvVars("XAGENT_JIRA_CLIENT_ID"),
},
&cli.StringFlag{
    Name:    "jira-client-secret",
    Usage:   "Atlassian OAuth client secret",
    Sources: cli.EnvVars("XAGENT_JIRA_CLIENT_SECRET"),
},
```

No global webhook secret flag — secrets are per-org.

Configuration passed to server:

```go
if jiraClientID := cmd.String("jira-client-id"); jiraClientID != "" {
    opts.Jira = &server.JiraConfig{
        ClientID:     jiraClientID,
        ClientSecret: cmd.String("jira-client-secret"),
    }
}
```

Add to `server.go`:

```go
type JiraConfig struct {
    ClientID     string
    ClientSecret string
}
```

And add `Jira *JiraConfig` field to both `Options` and `Server`.

### 10. Settings UI

Add Jira section to `webui/src/routes/settings.tsx`:

**Jira Account Card** — mirrors the GitHub Account card:

- If linked: show Atlassian account ID with an "Unlink" button (calls `unlinkJiraAccount` RPC)
- If not linked: show "Link Jira Account" button (links to `/jira/login`)
- Uses `getJiraAccount` query

**Jira Webhook Card** — new, per-org:

- Shows the webhook URL for the current org: `{baseURL}/webhook/jira?org={orgID}`
- Shows the current webhook secret (masked) or "No secret configured"
- "Generate Secret" button calls `generateJiraWebhookSecret` RPC and displays the new secret
- Instructions text explaining that the user needs to register this webhook URL in their Jira Cloud instance settings

Both cards use the same `useQuery` / `useMutation` patterns from `@connectrpc/connect-query` as the existing GitHub card.

### 11. Implementation Order

1. Database migration (`015_jira.sql`) + store queries
2. Model updates (`user.go` + `org.go`)
3. Proto definitions + `mise run generate`
4. Atlassian OAuth flow (`internal/jiraauth/`)
5. Webhook handler (`internal/webhook/jira.go`)
6. Server RPC handlers
7. Server HTTP route registration
8. CLI flags
9. Settings UI

## Trade-offs

### Per-org webhook secret vs global webhook secret

GitHub uses a global webhook secret because the GitHub App model has a single webhook endpoint per app. Jira Cloud webhooks are configured per-instance, and each org may have its own Jira instance. Storing the secret per-org means:

- Each org independently manages their Jira webhook integration
- The webhook URL includes `?org=<id>` to identify which secret to use for verification
- No global server config needed for the webhook secret
- Trade-off: requires a DB lookup on every webhook request (acceptable for webhook traffic volume)

### Atlassian account ID vs Jira user ID

Atlassian uses a single `account_id` across all their products (Jira, Confluence, etc.). Using `atlassian_account_id` rather than a Jira-specific ID future-proofs the integration if other Atlassian products are added later. The field name on the `users` table reflects this.

### No username caching for Jira

GitHub webhooks include the username, which xagent caches for display. Jira webhook payloads include `displayName` but it's less commonly used as an identifier. The current design stores only `atlassian_account_id`. Display name could be added later if needed.

### Shared vs duplicated webhook routing logic

The `findLinksByOrg` and `routeEventToLinks` methods on `GitHubHandler` could be extracted into shared helpers. However, they're relatively small and may diverge as each integration adds provider-specific logic. Starting with duplication is acceptable — extract if/when they stay identical after implementation.

## Open Questions

1. **Jira webhook event types:** The design covers `comment_created` and `comment_updated`. Should `issue_updated` (status changes, assignee changes) also trigger events? This could be added incrementally.

2. **xagent: prefix requirement:** GitHub webhooks require an `xagent:` prefix in comments to avoid noise. Should Jira follow the same convention, or is there a better filtering mechanism for Jira (e.g. mentioning a specific user, using a JQL filter)?

3. **Jira issue URL matching:** Jira issue URLs vary by instance (e.g. `https://mycompany.atlassian.net/browse/PROJ-123`). The existing `FindNotifyLinksByURLForUser` query does exact URL matching. Should we normalize Jira URLs or support pattern matching?
