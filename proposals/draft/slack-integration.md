# Slack Integration

## Problem

xagent has GitHub and Jira integrations that let users link their accounts and receive webhook events routed to tasks. There is no equivalent Slack integration. Users who coordinate work in Slack cannot receive notifications on their tasks when someone mentions xagent in a channel or thread, and agents cannot post updates back to Slack conversations.

## Design

This proposal adds Slack integration using the Slack Events API for inbound events and the Slack Web API for outbound messages. The pattern mirrors the existing GitHub integration (global app-level signing secret, OAuth account linking on the user) with the addition of per-org bot token storage for bidirectional communication.

### Slack App Model

Unlike GitHub (single App webhook secret) or Jira (per-org webhook secret), Slack uses a single App with:

- **One signing secret** per app (global, like GitHub)
- **Per-workspace bot tokens** issued when the app is installed to a Slack workspace (stored per-org)
- **Events API** delivering events to a single Request URL

Each xagent org installs the Slack app to their workspace, which grants a bot token. The bot token is stored per-org and used for sending messages.

### 1. Database Migration

New migration `internal/store/sql/migrations/016_slack.sql`:

```sql
ALTER TABLE users ADD COLUMN slack_user_id TEXT;
CREATE UNIQUE INDEX idx_users_slack_user_id ON users(slack_user_id);

ALTER TABLE orgs ADD COLUMN slack_team_id TEXT NOT NULL DEFAULT '';
ALTER TABLE orgs ADD COLUMN slack_bot_token TEXT NOT NULL DEFAULT '';
```

**Rationale:** `slack_user_id` on the user follows the same pattern as `github_user_id` and `atlassian_account_id`. The bot token is per-org because each org installs the Slack app to their own workspace. `slack_team_id` maps a Slack workspace to an org for event routing.

### 2. Store Layer

Add to `internal/store/sql/queries/user.sql`:

```sql
-- name: GetUserBySlackUserID :one
SELECT id, email, name, github_user_id, github_username, slack_user_id, default_org_id, created_at, updated_at
FROM users
WHERE slack_user_id = $1;

-- name: LinkSlackAccount :exec
UPDATE users SET
    slack_user_id = $2,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UnlinkSlackAccount :exec
UPDATE users SET
    slack_user_id = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1;
```

Add to `internal/store/sql/queries/org.sql`:

```sql
-- name: GetOrgBySlackTeamID :one
SELECT id, name, owner, archived, slack_team_id, slack_bot_token, created_at, updated_at
FROM orgs
WHERE slack_team_id = $1;

-- name: SetOrgSlackInstallation :exec
UPDATE orgs SET
    slack_team_id = $2,
    slack_bot_token = $3,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: ClearOrgSlackInstallation :exec
UPDATE orgs SET
    slack_team_id = '',
    slack_bot_token = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id = $1;
```

All existing user queries must add `slack_user_id` to their SELECT lists.

### 3. Model Updates

Update `internal/model/user.go`:

```go
type User struct {
    ID               string    `json:"id"`
    Email            string    `json:"email"`
    Name             string    `json:"name"`
    GitHubUserID     int64     `json:"github_user_id"`
    GitHubUsername   string    `json:"github_username"`
    SlackUserID      string    `json:"slack_user_id"`
    DefaultOrgID     int64     `json:"default_org_id"`
    CreatedAt        time.Time `json:"created_at"`
    UpdatedAt        time.Time `json:"updated_at"`
}

func (u *User) HasSlack() bool {
    return u.SlackUserID != ""
}

func (u *User) SlackAccountProto() *xagentv1.SlackAccount {
    if !u.HasSlack() {
        return nil
    }
    return &xagentv1.SlackAccount{
        SlackUserId: u.SlackUserID,
        CreatedAt:   timestamppb.New(u.CreatedAt),
    }
}
```

Update `internal/model/org.go`:

```go
type Org struct {
    ID             int64     `json:"id"`
    Name           string    `json:"name"`
    Owner          string    `json:"owner"`
    Archived       bool      `json:"archived"`
    SlackTeamID    string    `json:"slack_team_id"`
    SlackBotToken  string    `json:"slack_bot_token"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}
```

### 4. Proto Definitions

Add to `proto/xagent/v1/xagent.proto`:

```protobuf
// In service XAgentService:
rpc GetSlackAccount(GetSlackAccountRequest) returns (GetSlackAccountResponse);
rpc UnlinkSlackAccount(UnlinkSlackAccountRequest) returns (UnlinkSlackAccountResponse);
rpc GetSlackInstallation(GetSlackInstallationRequest) returns (GetSlackInstallationResponse);
rpc UninstallSlack(UninstallSlackRequest) returns (UninstallSlackResponse);

// Messages:
message SlackAccount {
  string slack_user_id = 1;
  google.protobuf.Timestamp created_at = 2;
}

message GetSlackAccountRequest {}
message GetSlackAccountResponse {
  SlackAccount account = 1;
}

message UnlinkSlackAccountRequest {}
message UnlinkSlackAccountResponse {}

message SlackInstallation {
  string team_id = 1;
}

message GetSlackInstallationRequest {}
message GetSlackInstallationResponse {
  SlackInstallation installation = 1;
}

message UninstallSlackRequest {}
message UninstallSlackResponse {}
```

`GetSlackInstallation` is org-scoped — returns whether the current org has a Slack workspace connected. No secret/token is exposed to the UI.

### 5. Slack OAuth Flow

Create `internal/slackauth/slackauth.go` mirroring `internal/ghauth/ghauth.go`:

```go
package slackauth

type Config struct {
    ClientID     string
    ClientSecret string
    RedirectURL  string
    Log          *slog.Logger
    OnSuccess    func(w http.ResponseWriter, r *http.Request, teamID string, botToken string, userID string)
}

type Handler struct {
    oauth     *oauth2.Config
    log       *slog.Logger
    onSuccess func(w http.ResponseWriter, r *http.Request, teamID string, botToken string, userID string)
    mux       *http.ServeMux
}
```

Key differences from `ghauth`:

- **OAuth endpoint:** `https://slack.com/oauth/v2/authorize` / `https://slack.com/api/oauth.v2.access`
- **Scopes (bot):** `app_mentions:read`, `channels:history`, `chat:write`, `users:read`
- **User scopes:** None required (user identity comes from the `authed_user` field in the OAuth response)
- **State cookie:** `xagent_slack_state` (same TTL and security flags as GitHub)
- **Routes:** `/login` and `/callback` (mounted at `/slack/`)
- **Token response:** Slack's `oauth.v2.access` returns `team.id`, `access_token` (bot token), and `authed_user.id` (Slack user ID) in a single response — no separate user fetch needed

The `/callback` handler:
1. Exchange code for token via `https://slack.com/api/oauth.v2.access`
2. Extract `team.id`, `access_token`, and `authed_user.id` from the response
3. Call `OnSuccess` with all three values
4. The server's `OnSuccess` callback stores the bot token on the org and links the Slack user ID to the user

### 6. Webhook Handler

Create `internal/webhook/slack.go` mirroring `internal/webhook/github.go`:

```go
type SlackHandler struct {
    Log           *slog.Logger
    Store         *store.Store
    SigningSecret string
}
```

Endpoint: `POST /webhook/slack`

**Request verification:** Slack uses a different signing scheme than GitHub:
1. Concatenate `v0:{timestamp}:{body}` where timestamp is the `X-Slack-Request-Timestamp` header
2. Compute HMAC-SHA256 using the app's signing secret
3. Compare against `X-Slack-Signature` header (prefixed with `v0=`)
4. Reject if timestamp is more than 5 minutes old (replay protection)

**URL verification challenge:** Slack sends a `url_verification` event when the Request URL is first configured. The handler must respond with the `challenge` value:

```go
if payload.Type == "url_verification" {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"challenge": payload.Challenge})
    return
}
```

**Processing steps for `event_callback` type:**

1. Verify request signature
2. Parse the outer envelope (`type`, `team_id`, `event`)
3. Extract inner event (see supported events below)
4. Look up org by `slack_team_id` to determine the org scope
5. Enforce `xagent:` prefix on message text (same pattern as GitHub)
6. Look up user by `slack_user_id` using `GetUserBySlackUserID`
7. If not found, ignore (user hasn't linked their Slack account)
8. Call `findLinksByOrg()` to find matching notify links
9. Create event and route to tasks using `routeEventToLinks()`

**Supported Slack event types:**

- `app_mention` — Someone mentioned @xagent in a channel. Extract `user` (Slack user ID), `text` (message body), and `channel` (channel ID). Construct the URL as `https://slack.com/archives/{channel}/p{ts}` (Slack message permalink format).
- `message` (in threads only) — A reply in a thread where xagent was previously mentioned. Extract the same fields plus `thread_ts`.

The `extractSlackEvent` function:

```go
type slackEvent struct {
    description  string
    data         string
    url          string
    slackUserID  string
}
```

**URL matching:** The event URL is constructed from the channel ID and message timestamp. For tasks to receive Slack events, they need a notify link with a matching Slack permalink URL. This happens when:
- An agent posts a message to Slack and creates a notify link with the message URL
- A user manually adds a Slack permalink as a notify link on a task

**Deduplication:** Use the Slack `event_id` field from the outer envelope. Slack may retry delivery, so the handler should check for duplicate `event_id` values. A simple approach: include the event_id in the event description and rely on the existing event-task deduplication (same event won't be routed twice to the same task).

### 7. Outbound Messages (MCP Tool)

Add a `slack_post_message` tool to the xagent MCP server that agents can use to post messages back to Slack:

In `internal/servermcp/tools.go`:

```go
// slack_post_message - Post a message to a Slack channel or thread
{
    Name:        "slack_post_message",
    Description: "Post a message to a Slack channel or thread",
    InputSchema: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "channel": map[string]any{
                "type":        "string",
                "description": "Slack channel ID (e.g. C01234567)",
            },
            "text": map[string]any{
                "type":        "string",
                "description": "Message text (supports Slack markdown)",
            },
            "thread_ts": map[string]any{
                "type":        "string",
                "description": "Thread timestamp to reply in a thread (optional)",
            },
        },
        "required": []string{"channel", "text"},
    },
}
```

The handler:
1. Look up the task's org
2. Fetch the org's `slack_bot_token`
3. Call `https://slack.com/api/chat.postMessage` with the bot token
4. Return the message permalink
5. Optionally create a notify link on the task for the posted message (so replies route back)

This is the key feature that enables the bidirectional workflow: agent posts to Slack, creates a notify link, and receives follow-up replies as events.

### 8. Server RPC Handlers

**GetSlackAccount:** Return the current user's linked Slack user ID (mirrors `GetGitHubAccount`).

**UnlinkSlackAccount:** Clear `slack_user_id` on the caller's user record.

**GetSlackInstallation:** Return the current org's `slack_team_id` if configured. Does not expose the bot token.

**UninstallSlack:** Clear `slack_team_id` and `slack_bot_token` on the current org. This effectively disconnects the Slack workspace.

### 9. Server HTTP Routes

Add to `server.Handler()`:

```go
// Slack OAuth routes (conditional on Slack config)
if s.slack != nil {
    sh := slackauth.New(slackauth.Config{
        ClientID:     s.slack.ClientID,
        ClientSecret: s.slack.ClientSecret,
        RedirectURL:  s.baseURL + "/slack/callback",
        Log:          s.log,
        OnSuccess: func(w http.ResponseWriter, r *http.Request, teamID, botToken, userID string) {
            caller := apiauth.Caller(r.Context())
            if caller == nil {
                http.Error(w, "not authenticated", http.StatusUnauthorized)
                return
            }
            // Store bot token on the org
            if err := s.store.SetOrgSlackInstallation(r.Context(), nil, caller.OrgID, teamID, botToken); err != nil {
                http.Error(w, "failed to install Slack", http.StatusInternalServerError)
                return
            }
            // Link Slack user ID to the user
            if err := s.store.LinkSlackAccount(r.Context(), nil, caller.ID, userID); err != nil {
                http.Error(w, "failed to link Slack account", http.StatusInternalServerError)
                return
            }
            http.Redirect(w, r, "/ui/settings", http.StatusFound)
        },
    })
    mux.Handle("/slack/", alice.New(s.auth.RequireAuth(), s.auth.AttachUserInfo()).Then(http.StripPrefix("/slack", sh)))
}

// Slack webhook (always registered — uses global signing secret)
if s.slack != nil {
    mux.Handle("/webhook/slack", &webhook.SlackHandler{
        Log:           s.log,
        Store:         s.store,
        SigningSecret: s.slack.SigningSecret,
    })
}
```

**Note:** Unlike the Jira webhook (which is always registered because secrets are per-org), the Slack webhook is only registered when Slack config is present because the signing secret is global.

### 10. CLI Flags

Add to `internal/command/server.go`:

```go
&cli.StringFlag{
    Name:    "slack-client-id",
    Usage:   "Slack App OAuth client ID",
    Sources: cli.EnvVars("XAGENT_SLACK_CLIENT_ID"),
},
&cli.StringFlag{
    Name:    "slack-client-secret",
    Usage:   "Slack App OAuth client secret",
    Sources: cli.EnvVars("XAGENT_SLACK_CLIENT_SECRET"),
},
&cli.StringFlag{
    Name:    "slack-signing-secret",
    Usage:   "Slack App signing secret (for webhook verification)",
    Sources: cli.EnvVars("XAGENT_SLACK_SIGNING_SECRET"),
},
```

Configuration passed to server:

```go
if slackClientID := cmd.String("slack-client-id"); slackClientID != "" {
    opts.Slack = &server.SlackConfig{
        ClientID:      slackClientID,
        ClientSecret:  cmd.String("slack-client-secret"),
        SigningSecret: cmd.String("slack-signing-secret"),
    }
}
```

Add to `server.go`:

```go
type SlackConfig struct {
    ClientID      string
    ClientSecret  string
    SigningSecret string
}
```

And add `Slack *SlackConfig` field to both `Options` and `Server`.

### 11. Settings UI

Add Slack section to `webui/src/routes/settings.tsx`:

**Slack Account Card** — mirrors the GitHub Account card:

- If linked: show Slack user ID with an "Unlink" button (calls `unlinkSlackAccount` RPC)
- If not linked: show "Link Slack Account" button (links to `/slack/login`)
- Uses `getSlackAccount` query

**Slack Workspace Card** — per-org:

- If installed: show Slack team ID with an "Uninstall" button (calls `uninstallSlack` RPC)
- If not installed: show "Install to Slack" button (links to `/slack/login` — same OAuth flow installs the app and links the user)
- Note: The OAuth flow handles both workspace installation and user linking in a single step

### 12. Implementation Order

1. Database migration (`016_slack.sql`) + store queries
2. Model updates (`user.go` + `org.go`)
3. Proto definitions + `mise run generate`
4. Slack OAuth flow (`internal/slackauth/`)
5. Webhook handler (`internal/webhook/slack.go`)
6. Server RPC handlers
7. Server HTTP route registration + CLI flags
8. `slack_post_message` MCP tool
9. Settings UI

## Trade-offs

### Global signing secret vs per-org secret

Slack apps have a single signing secret per app, so unlike Jira (where each org has its own Jira instance with its own webhook secret), all orgs share the same Slack app and signing secret. The signing secret is configured as a global server flag. This is the same model as the GitHub integration.

The alternative — supporting multiple Slack apps — adds significant complexity for little benefit. Most deployments will use a single Slack app installed to multiple workspaces.

### Bot token storage

Storing bot tokens in the database means they need to be protected. The `slack_bot_token` column should be encrypted at rest (using the existing `encryption_key` mechanism if available). The token is never exposed through the API — `GetSlackInstallation` only returns the team ID.

An alternative is to require users to configure bot tokens via environment variables, but this doesn't scale to multiple orgs with different Slack workspaces.

### Event URL construction

Slack events don't include a permalink URL — the handler constructs one from the channel ID and message timestamp (`https://slack.com/archives/{channel}/p{ts_without_dot}`). This constructed URL must exactly match notify links created by agents. This is somewhat fragile but is the standard Slack permalink format.

An alternative is to call the `chat.getPermalink` API for each event, but this adds latency and an API call per webhook.

### MCP tool for outbound messages vs separate Slack bot

The proposal adds `slack_post_message` as an MCP tool rather than running a separate Slack bot process. This keeps the architecture simple — agents post to Slack through the same MCP server they already use. A separate bot would add deployment complexity and a new long-running process.

### Thread-based routing

The design routes events based on Slack message permalinks. When an agent posts to Slack and creates a notify link, thread replies to that message will route back to the task. This naturally maps to the existing link + event system without requiring Slack-specific routing logic.

The limitation is that someone can't just mention @xagent in an arbitrary channel and have it create a task — there must be an existing task with a matching notify link. Creating tasks from Slack mentions could be added as a future enhancement.

## Open Questions

1. **Bot token encryption:** Should `slack_bot_token` be encrypted in the database using the server's encryption key? The GitHub integration doesn't store long-lived tokens (it uses the App's webhook secret), so there's no precedent for token storage. The Jira integration stores only a webhook secret, not an API token.

2. **Multiple workspace support:** Can a single org be connected to multiple Slack workspaces? The current design stores one `slack_team_id` per org. Supporting multiple workspaces would require a separate `slack_installations` table.

3. **Slack user display name:** Should we cache the Slack display name on the user record (like `github_username`)? This would require an extra API call during OAuth or webhook processing.

4. **Rate limiting:** Slack has strict rate limits on `chat.postMessage` (roughly 1 message per second per channel). Should the `slack_post_message` MCP tool implement any client-side rate limiting, or rely on Slack's 429 responses?

5. **Channel-level links vs message-level links:** Should tasks be linkable to entire channels (receive all `xagent:` messages in a channel) or only to specific message threads? Channel-level links would be simpler for users but noisier.
