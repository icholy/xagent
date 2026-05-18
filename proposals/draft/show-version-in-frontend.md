# Show Server Version in the Frontend

Issue: https://github.com/icholy/xagent/issues/601

## Problem

There is no way to see which version of xagent is running from the web UI. The CLI exposes version via `xagent version` using `runtime/debug.ReadBuildInfo()`, but this information is not available to the frontend.

## Design

### 1. Extend `PingResponse` with a version field

In `proto/xagent/v1/xagent.proto`, add a `version` field to the existing `PingResponse`:

```protobuf
message PingResponse {
  string version = 1;
}
```

The `Ping` RPC is already defined and the frontend has generated client code for it. Adding a field to the response is backward-compatible.

### 2. Populate version in the server

Extract the version resolution logic from `internal/command/version.go` into a shared function (e.g. in a new `internal/version/version.go` package):

```go
package version

import "runtime/debug"

// String returns the build version.
func String() string {
    info, ok := debug.ReadBuildInfo()
    if !ok {
        return "(unknown)"
    }
    v := info.Main.Version
    if v == "" || v == "(devel)" {
        for _, s := range info.Settings {
            if s.Key == "vcs.revision" {
                v = s.Value
                if len(v) > 8 {
                    v = v[:8]
                }
                return v
            }
        }
        return "(devel)"
    }
    return v
}
```

Update the `Ping` handler in `internal/server/apiserver/apiserver.go`:

```go
func (s *Server) Ping(ctx context.Context, req *xagentv1.PingRequest) (*xagentv1.PingResponse, error) {
    return &xagentv1.PingResponse{
        Version: version.String(),
    }, nil
}
```

Update `internal/command/version.go` to use the shared function.

### 3. Display version in the navbar

The `ConnectionIndicator` component in `webui/src/components/connection-indicator.tsx` already sits in the navbar and communicates server connectivity status. Extend its tooltip to also show the server version:

- Call the `ping` RPC once on mount (via `useQuery` with a long `staleTime`)
- Show the version string in the tooltip alongside the connection state label (e.g. "Connected - v0.14.1")
- The dot indicator itself stays unchanged

This approach avoids adding new UI elements. The version is one hover away from any page.

```tsx
export function ConnectionIndicator() {
  const state = useConnectionState();
  const { data } = useQuery(ping, {}, { staleTime: Infinity });
  const { color, label, pulse } = styles[state];
  const tooltip = data?.version ? `${label} (${data.version})` : label;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          aria-label={label}
          className={cn("inline-block h-2 w-2 rounded-full", color, pulse && "animate-pulse")}
        />
      </TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}
```

### Files Changed

| File | Change |
|------|--------|
| `proto/xagent/v1/xagent.proto` | Add `version` field to `PingResponse` |
| `internal/version/version.go` | New package with shared version resolution |
| `internal/command/version.go` | Use `version.String()` |
| `internal/server/apiserver/apiserver.go` | Return version in `Ping` |
| `webui/src/components/connection-indicator.tsx` | Show version in tooltip |

## Trade-offs

**Adding a dedicated `GetVersion` RPC vs. extending `PingResponse`**: A separate RPC would be more explicit, but `Ping` is already the lightweight health-check endpoint and adding a string field keeps things simple. If more build metadata is needed later (commit hash, build time), a dedicated `GetServerInfo` RPC can be introduced at that point.

**Showing version in the tooltip vs. always visible in the navbar**: Always-visible text takes up navbar space on every page. A tooltip on the already-present connection dot is unobtrusive and discoverable. If more prominent placement is desired later, a footer or settings page "About" section can be added.

## Open Questions

1. Should the version also be shown on the settings page for easier copy-paste (e.g. for bug reports)?
2. Should additional build metadata (git commit, build time) be exposed alongside the version?
