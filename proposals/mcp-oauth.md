# OAuth 2.1 for MCP endpoint

- Status: pending
- Issue: https://github.com/icholy/xagent/issues/417

## Problem

The `/mcp` endpoint cannot be used as a Claude.ai custom connector because Claude.ai requires OAuth 2.1 authorization code flow with PKCE. The current auth mechanisms (API key with `X-Auth-Type` header, Zitadel Bearer tokens, cookie sessions) are not compatible with Claude.ai's connector UI.

## Design

### Overview

Implement a minimal OAuth 2.1 authorization server within xagent. The user authenticates by entering their `xat_` API key on a frontend-rendered authorize page. The `client_id` and `client_secret` configured in Claude.ai's connector settings map to an API key. No Zitadel dependency.

### New Routes

All OAuth routes live under `/oauth/` and the well-known discovery paths:

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/.well-known/oauth-authorization-server` | Public | RFC 8414 metadata |
| GET | `/.well-known/oauth-protected-resource` | Public | RFC 9728 resource metadata |
| POST | `/oauth/authorize` | Public | Validates API key, issues auth code, redirects |
| POST | `/oauth/token` | Public | Exchanges auth code for app JWT |

The `GET /oauth/authorize` page is handled entirely by the React frontend (a new route in the SPA). The backend only handles the `POST`.

### Discovery Metadata

`GET /.well-known/oauth-authorization-server` returns:

```json
{
  "issuer": "https://xagent.example.com",
  "authorization_endpoint": "https://xagent.example.com/oauth/authorize",
  "token_endpoint": "https://xagent.example.com/oauth/token",
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code"],
  "code_challenge_methods_supported": ["S256"]
}
```

`GET /.well-known/oauth-protected-resource` returns:

```json
{
  "resource": "https://xagent.example.com",
  "authorization_servers": ["https://xagent.example.com"]
}
```

### Authorization Endpoint

**Frontend (`/ui/authorize`)**: React page that renders a form. Receives standard OAuth query params (`client_id`, `redirect_uri`, `state`, `code_challenge`, `code_challenge_method`, `response_type`). The form has a single input field for the user's API key and a submit button. On submit, POSTs to `/oauth/authorize`.

Note: the `authorization_endpoint` in the metadata points to `/oauth/authorize`, but that path serves the SPA which handles the `/authorize` route client-side. The SPA form POSTs to `/oauth/authorize` on the backend.

**Backend (`POST /oauth/authorize`)**: Accepts form data with the API key and the OAuth params from the frontend. Validates the API key via `HashKey()` + `KeyValidator`. On success, generates a random auth code, stores it in memory with the associated `UserInfo`, `code_challenge`, `client_id`, and `redirect_uri`, then redirects to `redirect_uri?code=<code>&state=<state>`.

### Token Endpoint

`POST /oauth/token` accepts `application/x-www-form-urlencoded`:

| Parameter | Description |
|-----------|-------------|
| `grant_type` | Must be `authorization_code` |
| `code` | The auth code from the authorize step |
| `client_id` | The client ID configured in Claude.ai |
| `client_secret` | The client secret (an `xat_` API key) |
| `code_verifier` | PKCE verifier matching the original `code_challenge` |
| `redirect_uri` | Must match the original `redirect_uri` |

Validation steps:
1. Look up the auth code in the in-memory store
2. Verify `client_id` and `redirect_uri` match the stored values
3. Verify `code_verifier` against stored `code_challenge` (SHA256)
4. Validate `client_secret` as an API key via `HashKey()` + `KeyValidator`
5. Delete the auth code (single use)

On success, sign an app JWT using the existing `SignAppToken()` with the `UserInfo` from the auth code and return:

```json
{
  "access_token": "<jwt>",
  "token_type": "Bearer",
  "expires_in": 300
}
```

### Auth Code Store

A simple in-memory store with expiration. Auth codes expire after 60 seconds and are single-use.

```go
type authCode struct {
    UserInfo      *apiauth.UserInfo
    ClientID      string
    RedirectURI   string
    CodeChallenge string
    ExpiresAt     time.Time
}

type codeStore struct {
    mu    sync.Mutex
    codes map[string]*authCode // code string -> authCode
}
```

No database migration needed. Auth codes are ephemeral and don't survive server restarts, which is fine -- Claude.ai will just re-initiate the flow.

### MCP Endpoint Auth Change

The `/mcp` endpoint currently requires the `X-Auth-Type` header to determine auth strategy. Claude.ai sends a plain `Authorization: Bearer <token>` with no custom headers.

Modify the `RequireAuth` middleware's default case (no `X-Auth-Type` header): after cookie auth fails, attempt app JWT validation via `VerifyAppToken()`. This allows the MCP endpoint to accept app JWTs issued by the OAuth token endpoint without any custom headers.

```go
// In RequireAuth, the default case (no X-Auth-Type header):
default:
    if !a.useDevUser(w, r, next) {
        // Try cookie auth first
        // If no cookie session, try app JWT from Bearer header
        // Fall back to cookie middleware (which will redirect to login)
    }
```

### New Package

`internal/mcpauth/` containing:

- `mcpauth.go` -- `Server` struct, `Options`, `New()` constructor
- `code.go` -- auth code store with expiry and cleanup
- `handlers.go` -- HTTP handlers for all 4 endpoints

The `Server` struct takes:

```go
type Options struct {
    KeyValidator apiauth.KeyValidator
    AppKey       ed25519.PrivateKey
    BaseURL      string
}
```

All dependencies are existing interfaces and types. No new store queries or proto changes.

### Route Registration in `server.go`

```go
mcpOAuth := mcpauth.New(mcpauth.Options{
    KeyValidator: &storeKeyValidator{store: s.store},
    AppKey:       appKey,
    BaseURL:      s.baseURL,
})
mux.HandleFunc("/.well-known/oauth-authorization-server", mcpOAuth.HandleMetadata)
mux.HandleFunc("/.well-known/oauth-protected-resource", mcpOAuth.HandleResourceMetadata)
mux.HandleFunc("/oauth/authorize", mcpOAuth.HandleAuthorize)
mux.HandleFunc("/oauth/token", mcpOAuth.HandleToken)
```

The `/oauth/authorize` GET request serves the SPA (needs a route addition so the SPA handles `/oauth/authorize` the same way it handles `/ui/*`), while the POST is handled by the backend.

### Frontend Route

New React route at `webui/src/routes/oauth.authorize.tsx`. Minimal page:

- Reads OAuth query params from the URL
- Renders a form with an API key input field
- POSTs the API key + OAuth params to `/oauth/authorize`
- Shows error state if the key is invalid

This page is **not** behind auth middleware -- it must be publicly accessible since the user arrives here via a browser redirect from Claude.ai.

### User Flow

1. User creates an API key in xagent UI (existing flow)
2. User adds a custom connector in Claude.ai with:
   - **URL**: `https://xagent.example.com/mcp`
   - **Client ID**: any string (e.g. `claude`)
   - **Client Secret**: their `xat_` API key
3. Claude.ai fetches discovery metadata, opens browser to `/oauth/authorize`
4. User enters their API key on the xagent authorize page
5. xagent validates the key, redirects back to Claude.ai with an auth code
6. Claude.ai exchanges the code + client secret at `/oauth/token` for an app JWT
7. Claude.ai uses the JWT as Bearer token on `/mcp`

## Trade-offs

### API key on the authorize page vs. cookie session auto-approve

The user must enter their API key on the authorize page, even if they're already logged into the xagent UI. An alternative would be to detect an existing cookie session and auto-approve (or show a consent screen). This would be more convenient but adds complexity -- the authorize page would need to handle both authenticated and unauthenticated states, and the cookie auth middleware would need to be wired into the OAuth flow. The API key approach is simpler and self-contained. Could be added later as an enhancement.

### In-memory auth code store vs. database

Auth codes are stored in memory, not in the database. This means auth codes don't survive server restarts, but since they're only valid for 60 seconds and are part of an interactive flow, this is acceptable. It avoids a database migration and keeps the implementation simple.

### client_id is not validated against the database

The `client_id` in this design is essentially ignored beyond matching it between the authorize and token steps. It's not looked up in any database. The actual authentication happens via the API key (entered on the authorize page and as `client_secret`). This is intentional -- there's no need for a separate client registry when API keys already provide identity and org scoping.

### No refresh tokens

The initial implementation does not issue refresh tokens. App JWTs have a 5-minute TTL (`apiauth.AppTokenTTL`). When the token expires, Claude.ai will need to re-initiate the OAuth flow. Refresh tokens could be added later if the re-auth frequency is a problem.

## Open Questions

1. **Should the authorize page use the existing cookie session?** If the user is already logged into the xagent UI, should the authorize page auto-approve instead of asking for an API key? This would be more convenient but requires the authorize page to be behind optional cookie auth middleware.

2. **Should refresh tokens be supported?** The MCP spec says servers "should support token expiry and refresh" for the best experience. The current `AppTokenTTL` is 5 minutes. Without refresh tokens, Claude.ai will re-trigger the full OAuth flow every 5 minutes, which may be disruptive.

3. **SPA routing for `/oauth/authorize`**: The authorize page needs to be served outside the `/ui/` prefix and without auth middleware. This may require adjusting how the SPA is served, or serving it at `/ui/oauth/authorize` and pointing the metadata `authorization_endpoint` there instead.
