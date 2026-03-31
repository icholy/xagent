# Server Bootstrap for Docker Compose

- Status: pending
- Issue: https://github.com/icholy/xagent/issues/411

## Problem

When running xagent via `docker compose up`, the runner needs an API key to authenticate with the server. Currently, the flow requires:

1. Start the server
2. Manually create an org and API key through the API
3. Pass that API key to the runner via `XAGENT_API_KEY`

This means `docker compose up` doesn't work as a single command. The runner crash-loops with "not authenticated, run setup first or provide -key flag" until someone manually provisions credentials.

Additionally, the runner requires an Ed25519 private key (`cfg.PrivateKey`) for signing agent JWTs used in container-to-runner communication. Without a config file or way to provide this key, the runner fails with "no private key configured, run setup first".

## Design

### Overview

When the server starts with `--no-auth`, it checks whether any org exists in the database. If not, it bootstraps a default org (using the dev user) and creates an API key. The raw API key value comes from the `XAGENT_BOOTSTRAP_API_KEY` environment variable so that both the server and runner can share the same `.env` file. The runner is also updated to auto-generate a private key when one isn't configured, since in `--no-auth` mode the key only needs to be consistent within a single runner process lifetime.

### Server-Side Bootstrap

Add a `bootstrap` function in `internal/command/server.go` that runs after the dev user is provisioned (inside the `if noAuth` block, around line 137):

```go
if noAuth {
    slog.Warn("SSO authentication disabled, using dev user")
    devUser = &apiauth.UserInfo{
        ID:    "dev",
        Email: "dev@localhost",
        Name:  "Developer",
    }
    if err := resolver.Provision(ctx, devUser); err != nil {
        return fmt.Errorf("failed to provision dev user: %w", err)
    }
    // Bootstrap API key if configured
    if bootstrapKey := cmd.String("bootstrap-api-key"); bootstrapKey != "" {
        if err := bootstrapAPIKey(ctx, st, devUser.ID, bootstrapKey); err != nil {
            return fmt.Errorf("failed to bootstrap API key: %w", err)
        }
    }
}
```

The `bootstrapAPIKey` function:

```go
func bootstrapAPIKey(ctx context.Context, st *store.Store, userID string, rawKey string) error {
    keyHash := apiauth.HashKey(rawKey)

    // Check if key already exists (idempotent)
    _, err := st.GetKeyByHash(ctx, nil, keyHash)
    if err == nil {
        slog.Info("bootstrap API key already exists")
        return nil
    }

    // Get the user's default org
    u, err := st.GetUser(ctx, nil, userID)
    if err != nil {
        return fmt.Errorf("get user: %w", err)
    }

    key := &model.Key{
        ID:        uuid.NewString(),
        Name:      "bootstrap",
        TokenHash: keyHash,
        OrgID:     u.DefaultOrgID,
    }
    if err := st.CreateKey(ctx, nil, key); err != nil {
        return fmt.Errorf("create key: %w", err)
    }
    slog.Info("bootstrapped API key", "org_id", u.DefaultOrgID)
    return nil
}
```

Key properties:
- **Idempotent**: checks if the key hash already exists before creating. Safe across server restarts.
- **Depends on dev user provisioning**: `storeUserResolver.Provision` already creates a default org if one doesn't exist (lines 249-269 in `server.go`), so `u.DefaultOrgID` is guaranteed to be set.
- **No expiration**: the bootstrap key has no expiry, matching dev usage patterns.

### New CLI Flag

Add a `--bootstrap-api-key` flag to the server command:

```go
&cli.StringFlag{
    Name:    "bootstrap-api-key",
    Usage:   "API key to bootstrap when using --no-auth (raw token value)",
    Sources: cli.EnvVars("XAGENT_BOOTSTRAP_API_KEY"),
},
```

This flag is only meaningful when `--no-auth` is also set. If `--no-auth` is not set and `--bootstrap-api-key` is provided, it should be silently ignored (bootstrap only makes sense in dev mode).

### Runner Private Key Auto-Generation

The runner currently requires a private key from the config file (`configfile.Load()`). In `internal/command/runner.go`, the runner fails at line 109 if `cfg.PrivateKey == nil`.

This private key is used by the runner's proxy (`internal/runner/proxy.go`) to sign JWTs for agent-to-runner communication. The key doesn't need to be persisted — it only needs to be consistent within a single runner process. The agent receives its signed JWT when the container starts, and the same runner process verifies it.

Update the runner command to auto-generate a private key when none is configured:

```go
cfg, err := configfile.Load()
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}
if cmd.IsSet("key") {
    cfg.Token = cmd.String("key")
}
if cfg.Token == "" {
    return fmt.Errorf("not authenticated, run setup first or provide -key flag")
}
// Auto-generate private key if not configured
if cfg.PrivateKey == nil {
    cfg.PrivateKey, err = agentauth.CreatePrivateKey()
    if err != nil {
        return fmt.Errorf("failed to generate private key: %w", err)
    }
    slog.Info("auto-generated runner private key (not persisted)")
}
```

This removes the hard failure at line 109 and generates an ephemeral key instead. The key is not saved to disk — it only lives for the duration of the runner process. This is fine because:

1. The private key signs task JWTs that agents use to authenticate with the runner's proxy
2. Agents only communicate with the runner that started them
3. If the runner restarts, containers are reconciled and restarted anyway

### Docker Compose Changes

Update `docker-compose.yml` to add a runner service and shared bootstrap key:

```yaml
services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: xagent
      POSTGRES_PASSWORD: xagent
      POSTGRES_DB: xagent
      PGPORT: "5433"
    network_mode: host
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U xagent -p 5433"]
      interval: 5s
      timeout: 5s
      retries: 5

  server:
    build:
      context: .
      target: server
    command: ["./xagent", "server", "--no-auth"]
    network_mode: host
    env_file: .env
    environment:
      XAGENT_DATABASE_URL: postgres://xagent:xagent@localhost:5433/xagent?sslmode=disable
      XAGENT_BOOTSTRAP_API_KEY: ${XAGENT_BOOTSTRAP_API_KEY:-xat_bootstrap_dev_key}
    depends_on:
      postgres:
        condition: service_healthy

  runner:
    build:
      context: .
      target: server
    command: ["./xagent", "runner"]
    network_mode: host
    env_file: .env
    environment:
      XAGENT_API_KEY: ${XAGENT_BOOTSTRAP_API_KEY:-xat_bootstrap_dev_key}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    depends_on:
      - server

volumes:
  postgres_data:
```

Key points:
- Both services reference `XAGENT_BOOTSTRAP_API_KEY` with a default fallback value, so it works without a `.env` file
- The default key `xat_bootstrap_dev_key` is a static dev-only value (has the `xat_` prefix so `apiauth.IsKey` returns true)
- Users can override via `.env` file or environment for slightly better security
- The runner mounts the Docker socket for container management
- The runner depends on the server but not with `condition: service_healthy` — the runner already handles server unavailability via its poll loop and will retry

### API Key Format Consideration

The bootstrap key value (e.g., `xat_bootstrap_dev_key`) must have the `xat_` prefix. The `apiauth.IsKey()` function checks for this prefix to distinguish API key auth from other auth types. The `apiauth.HashKey()` function accepts any string, so the key doesn't need to be the standard 32-random-bytes hex format — it just needs the prefix.

The `AuthTransport` in `internal/xagentclient/transport.go` sets `X-Auth-Type: key` which routes to the key validation path in `RequireAuth` middleware. The middleware calls `validateKey` which hashes the raw token and looks it up. As long as the server bootstrapped the same raw value, authentication succeeds.

## Trade-offs

**Environment variable vs auto-generated key**: An auto-generated key (server generates random key, writes to a shared volume) was considered but rejected. It adds complexity (shared filesystem, race conditions, polling) and doesn't work well with `docker compose` since there's no clean way for the runner to wait for a file. A shared env var is simpler and more predictable.

**Static default key vs requiring `.env` file**: Using a hardcoded default (`xat_bootstrap_dev_key`) means `docker compose up` works with zero configuration. The downside is that anyone who knows the default key can access the API. This is acceptable because `--no-auth` is already a dev-only mode with no real security. Users who want a different key can set `XAGENT_BOOTSTRAP_API_KEY` in their environment or `.env` file.

**Always bootstrap vs only when no org exists**: The design always bootstraps (idempotently) when `--bootstrap-api-key` is set, rather than checking "is this a fresh database". This is simpler and handles edge cases like database resets or manual org deletion without special logic.

**Ephemeral runner private key vs persisted**: The runner's private key could be derived from a seed env var for determinism across restarts. But since runner restarts already trigger container reconciliation, an ephemeral key is sufficient and avoids adding another env var.

## Open Questions

1. Should the default bootstrap key value be hardcoded in `docker-compose.yml` or should a `.env.example` file be provided that users copy to `.env`?
2. Should the `--bootstrap-api-key` flag warn/error if used without `--no-auth`, or silently ignore it?
3. Should the runner's healthcheck in docker compose wait for the server to be reachable, or is the retry loop sufficient?
