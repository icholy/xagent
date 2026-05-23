# Auto-Repair Stale Container Network Attachment

Issue: https://github.com/icholy/xagent/issues/661

## Problem

Starting an existing task container can fail with:

```
failed to start container: Error response from daemon: failed to set up container networking: network <id> not found
```

When the docker-compose stack is torn down and brought back up (`docker compose down && up`), the compose-managed network (e.g. `xagent-config_default`) is recreated and gets a **new** network ID. Any pre-existing `xagent-<task-id>` container still pins the **old** network ID in its `NetworkSettings.Networks[<name>].NetworkID`, even though the endpoint is keyed by network NAME. On the next `ContainerStart`, Docker tries to attach to the old ID, fails, and leaves the container in `Exited`. Every retry fails the same way until the container is manually removed — which throws away its logs and the files we copied into it at create time.

The runner today wraps the failure in `internal/runner/runner.go`:

```go
if err := r.docker.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
    return fmt.Errorf("failed to start container: %w", err)
}
```

…and the task goes to `FAILED` with no recovery path.

## Design

### Overview

Detect the "network not found" failure inside `Runner.Start` and repair the container in place by re-attaching it to the live network(s) by NAME, then retry `ContainerStart` exactly once. If the retry still fails, the task fails as it does today.

The runner already knows the desired networks for the task via `ws.Container.Networks` (`internal/runner/workspace/workspace.go:167`), so the repair doesn't need to read anything new — it just needs to drive `NetworkDisconnect` + `NetworkConnect` on each name listed there.

### Detection: reactive, not proactive

Two strategies were considered:

1. **Proactive** — before every start, inspect the container, list each `NetworkSettings.Networks` entry, look up the live network ID for that name, and reattach if they differ.
2. **Reactive** — start the container normally; only reattach if `ContainerStart` returns a "network … not found" error.

Reactive wins:

- Proactive adds a `ContainerInspect` + N `NetworkInspect` round-trips to **every** start, including the >99% case where nothing is wrong.
- Proactive has its own false-positive trap: a network can be torn down between the proactive check and the start, so we'd still need the reactive path as a fallback. Adding both is strictly more code.
- The failure mode is loud and unambiguous: Docker returns a specific error string with a stable shape ("failed to set up container networking: network ... not found"). It's safe to match.

### Error matching

The Docker SDK does not type this error. The daemon returns it as a plain message string. We match conservatively:

```go
// internal/x/dockerx/network.go
//
// IsNetworkNotFoundOnStart reports whether err is the daemon's "network
// not found" error raised during container start. The daemon returns
// this as a plain string; there is no typed sentinel.
func IsNetworkNotFoundOnStart(err error) bool {
    if err == nil {
        return false
    }
    s := err.Error()
    return strings.Contains(s, "failed to set up container networking") &&
        strings.Contains(s, "not found")
}
```

Both substrings together avoid false positives from unrelated "not found" errors (image, container, volume). The message text is part of dockerd's stable user-facing surface — moby has carried this wording for years — but the helper is centralised in one file so a future Docker rename is a one-line fix.

### Repair helper

A new helper in `internal/x/dockerx/network.go`:

```go
// ReattachNetworks force-disconnects each named network from the container
// and reconnects it. Used to recover from stale network IDs left behind
// after `docker compose down && up` recreates the network.
//
// Disconnect uses Force=true so endpoints pinned to a no-longer-existent
// network ID can be dropped (a non-force disconnect would itself fail with
// "network not found").
//
// Disconnect/Connect are referenced by network NAME, which is what the
// workspace config supplies and what the live Docker network registry is
// keyed on after recreate.
func ReattachNetworks(ctx context.Context, c *client.Client, containerID string, networks []string) error {
    for _, name := range networks {
        // Best-effort disconnect: it's expected to "succeed" even when the
        // attached network ID is stale, because Force=true tells the daemon
        // to drop the endpoint without trying to talk to the missing network.
        if err := c.NetworkDisconnect(ctx, name, containerID, true); err != nil {
            // If the endpoint doesn't exist at all (e.g. we ran twice),
            // that's fine — we just need to end up connected.
            if !isNotConnectedErr(err) {
                return fmt.Errorf("disconnect %q: %w", name, err)
            }
        }
        if err := c.NetworkConnect(ctx, name, containerID, nil); err != nil {
            return fmt.Errorf("connect %q: %w", name, err)
        }
    }
    return nil
}
```

`NetworkConnect`'s last argument is `*network.EndpointSettings`. Passing `nil` produces the same default endpoint we get today from `Container.NetworkingConfig()` (the workspace passes `&network.EndpointSettings{}` per network — no aliases, no fixed IP, no driver opts — so `nil` is equivalent).

### Wire-up in `Runner.Start`

`internal/runner/runner.go:462` becomes:

```go
func (r *Runner) Start(ctx context.Context, task *model.Task) error {
    c, ok, err := r.find(ctx, task.ID)
    if err != nil {
        return err
    }

    var containerID string
    if ok {
        r.log.Info("starting existing container", "task", task.ID, "name", fmt.Sprintf("xagent-%d", task.ID))
        containerID = c.ID
    } else {
        containerID, err = r.create(ctx, task)
        if err != nil {
            return err
        }
    }

    err = r.docker.ContainerStart(ctx, containerID, container.StartOptions{})
    if err == nil {
        return nil
    }

    // Only the "existing container with stale network ID" path is repairable.
    // A freshly created container can't have a stale attachment.
    if !ok || !dockerx.IsNetworkNotFoundOnStart(err) {
        return fmt.Errorf("failed to start container: %w", err)
    }

    ws, wsErr := r.workspaces.Get(task.Workspace)
    if wsErr != nil {
        return fmt.Errorf("failed to start container: %w (and could not resolve workspace for repair: %v)", err, wsErr)
    }

    r.log.Warn("stale network attachment detected, repairing",
        "task", task.ID, "container", containerID, "networks", ws.Container.Networks, "err", err)

    if repairErr := dockerx.ReattachNetworks(ctx, r.docker, containerID, ws.Container.Networks); repairErr != nil {
        return fmt.Errorf("failed to start container: %w (repair failed: %v)", err, repairErr)
    }

    if err := r.docker.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
        return fmt.Errorf("failed to start container after network repair: %w", err)
    }
    r.log.Info("started after network repair", "task", task.ID, "container", containerID)
    return nil
}
```

Key properties:

- Repair runs **only** for the `ok == true` branch (existing container). A container we just created can't have a stale attachment, so falling into this path would mask real bugs.
- Repair is gated on the specific error. Any other start failure (image gone, port bind conflict, OOM, etc.) returns immediately as it does today.
- Retry runs **once**. If the post-repair start still fails, the error includes both the original error and the repair failure for debugging.

### Why reattach in place, not remove-and-recreate

Both strategies work. The proposal recommends **reattach in place**.

| Aspect | Reattach in place (proposed) | Remove and recreate |
|---|---|---|
| Container logs | Preserved | Lost |
| Files copied at create (`/usr/local/bin/xagent`, agent config, prebuilt binary, directory perms — see `Runner.create` lines 451-456) | Preserved | Re-copied (extra work, ~5-30MB depending on prebuilt) |
| New JWT minted? | No — existing token still valid | Yes — `r.proxy.TaskToken(task)` runs again |
| Image pull if registry changed | No | Yes (`dockerx.ImageEnsure`) |
| Concurrency cost | One disconnect + one connect per network (typically 1 network) | Full create path (create + copy files + start) |
| Code path | New 6-line helper + 1 branch in `Start` | Reuse existing `r.create()` |
| Robustness if container itself is corrupt | Weaker — only repairs the network endpoint | Stronger — fresh container |
| Idempotency on partial failure | Disconnect+Connect is naturally idempotent | Removal must succeed before recreate; partial removal is awkward |

The corruption scenario for "remove and recreate" is theoretical — a stale network ID does not imply a corrupt filesystem layer. In practice the container is fine and the only stale state is the network endpoint, so the cheaper, log-preserving repair is the right default.

If reattach itself fails (e.g. the network really is gone with no replacement) the start surfaces an error and the task fails as before — the same outcome as the current behaviour, with no regression.

### Edge cases

**Multiple networks.** `Container.Networks` is a slice and `NetworkingConfig()` attaches each one. The repair iterates the same slice in order. If the failure was caused by network A but network B is healthy, we still disconnect+reconnect B; that's a cheap redundant operation and keeps the code simple. (Alternative: inspect the container, compare each network's stored ID to the live ID, and only reattach the mismatched ones. Rejected as premature optimisation — the typical task uses one network.)

**Primary `NetworkMode` is immutable after create.** This is the key safety property that makes reattach viable. `container.HostConfig.NetworkMode` (set at create time and read by Docker as the primary network) cannot be changed without recreate. The workspace builder (`internal/runner/containerbuild/builder.go:50-54`) does **not** set `NetworkMode`, so each task container's primary attachment is Docker's default `bridge` network, and all workspace-configured networks come in through `NetworkingConfig.EndpointsConfig` — exactly the surface that `NetworkDisconnect`/`NetworkConnect` can mutate. If a future workspace setting introduces a non-default `NetworkMode`, reattach would not fix a stale primary network and the runner would have to fall back to remove-and-recreate. We add a TODO in the helper for this case but do not implement the fallback in this proposal; the workspace config does not expose `NetworkMode` today.

**Retry limits / loops.** Repair runs once per `Start` call. There is no in-loop retry. If repair fails, `Start` returns an error, the task goes to `FAILED`, and the runner does **not** keep retrying within the same poll tick. The next poll cycle (if the task is restarted) gets a fresh attempt — same as today. This bounds the recovery work and avoids any chance of a tight infinite loop on a truly broken host.

**Concurrent starts on the same container.** `Runner.Start` for a given task can only be invoked once per poll cycle (the poll loop iterates tasks; the runner's `sem` slots are acquired before `Start`). Two simultaneous `Runner.Start` calls for the same task ID would already be a bug independent of this change. `NetworkDisconnect`/`NetworkConnect` themselves are serialised inside dockerd per container, so even in a race the worst case is one of the two repairs no-ops the other.

**Container vanished between `find` and `start`.** Already handled by the existing error path: `ContainerStart` returns a "no such container" error, which does **not** match `IsNetworkNotFoundOnStart`, so we fall through to the existing failure path. (`find` is best-effort; we don't promise the container still exists when we try to start it.)

**Network truly gone with no replacement.** If `NetworkConnect` returns "network not found" itself, the original start error and the repair error are both included in the returned error string, the task fails, and a human can investigate. This is no worse than today.

### Logging and observability

- A `Warn` log on detection (`stale network attachment detected, repairing`) makes the repair visible in the runner logs without lifting it to `Error` (which would be noisy in environments that compose-recreate frequently).
- An `Info` log on success (`started after network repair`) confirms recovery.
- No new metric is added in this proposal; if the repair turns out to fire often in production, adding a `xagent_runner_network_repairs_total` counter is a follow-up.

### Tests

- Unit test for `IsNetworkNotFoundOnStart` against the real daemon's error string (constructed from a fixture, no docker required).
- Integration test in `internal/runner` exercising the full repair path: create a network, create a container attached to it, remove the network, recreate it under the same name, call `Runner.Start`, assert container is running and attached to the new network's ID. Gated behind the existing test docker dependency.

## Trade-offs

| Approach | Pros | Cons |
|---|---|---|
| **Reactive reattach in place** (proposed) | Preserves logs and copied files; cheap (no image pull, no file copy, no new token); single short helper; only runs on the specific failure | Relies on substring match against a daemon error message; doesn't help if `NetworkMode` is ever set to a stale custom value (not configurable today) |
| **Reactive remove-and-recreate** | Simpler — reuses `r.create()`; recovers from any container-level corruption, not just network | Loses container logs and copied state; ~5-30MB of extra file copy per repair; re-mints JWT; more wall-clock latency on the recovery |
| **Proactive precheck** | No reliance on error string matching | Adds inspect+lookup overhead to every start; still needs reactive fallback for races; significantly more code for marginal benefit |
| **Do nothing, document `docker rm` as the manual fix** | Zero code | The original status quo; every dev environment hits this after `docker compose down && up` |

## Open Questions

1. **Should repair also trigger on `NetworkMode` mismatches (not just `EndpointsConfig`)?** Today no workspace sets a non-default `NetworkMode`, so this is moot. If we add a workspace setting for it later, we'd need to fall back to remove-and-recreate when the primary mode is stale. Worth deciding before exposing `NetworkMode` in the workspace schema, not now.

2. **Should we log at `Warn` or `Info` on detection?** `Warn` makes it discoverable when investigating odd behaviour; `Info` keeps logs quieter in environments that recreate the stack daily. Proposal goes with `Warn` until we have data on frequency.

3. **Counter / metric.** Not added in this proposal. If the failure is rare in production we can leave it at log-only; if it's frequent we want a counter so on-call can spot a pattern. Easy follow-up.
