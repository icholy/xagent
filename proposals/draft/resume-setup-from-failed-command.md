# Resume Setup From Failed Command

Issue: https://github.com/icholy/xagent/issues/751

## Problem

Container setup re-runs **every** setup command on restart, including ones that already succeeded. When a non-idempotent command (e.g. `git clone`) succeeded on a prior run, re-running it on restart fails for a spurious reason and masks the original failure — so a restart can never recover.

The setup loop in `internal/agent/driver.go:58-73` is gated by a single all-or-nothing flag:

```go
if !cfg.Setup {
    for _, command := range cfg.Commands {
        d.Log.Info("Running setup command", "command", command)
        c := exec.CommandContext(ctx, "sh", "-c", command)
        c.Stdout = os.Stdout
        c.Stderr = os.Stderr
        if err := c.Run(); err != nil {
            return fmt.Errorf("setup command failed: %w", err)
        }
    }
    cfg.Setup = true
    if err := SaveConfig(d.TaskID, cfg); err != nil {
        return fmt.Errorf("failed to save config: %w", err)
    }
}
```

`cfg.Setup` is set to `true` **only** after every command in `cfg.Commands` returns zero. If command N fails, the flag stays `false` and the next run re-executes commands `0..N` from the top — re-running every already-succeeded, non-idempotent command. The failure trace in issue #751 (task 698) shows this exactly: run 1's `git clone` succeeded and `mise install` failed (transient 403); run 2's restart re-ran from index 0 and failed at `git clone` with `fatal: destination path 'xagent' already exists and is not an empty directory`. The real failure is never reached.

## Design

### Overview

Persist a per-command progress marker alongside the existing per-task config. Save it **after each successful command**, not just at the end. On (re)start, begin the loop at the saved resume point and skip already-completed commands. Set `cfg.Setup = true` only when the last command completes (unchanged semantics for the "fully done" state).

The workspace volume already persists across container restarts (that's why the half-done `git clone` artifacts remain on disk). The per-task config lives in the container's writable layer at `/tmp/xagent/<task-id>.json` (`internal/agent/config.go:13,68-70`) and is preserved across container restarts the same way — `Runner.create` only writes a fresh cfg when the container is being created from scratch (`internal/runner/runner.go:508`, in the `find`-miss branch of `Runner.Start`). So the progress marker rides alongside the existing fields and survives exactly the same restart cycles that today's `cfg.Setup` survives.

### Config schema

`internal/agent/config.go:15` extends the `Config` struct with two new agent-managed fields, sitting next to `Setup` / `Started`:

```go
type Config struct {
    // ... existing runner-provided fields (Commands, Cwd, McpServers, …) unchanged

    // Agent-managed state
    Setup                  bool   `json:"setup,omitempty"`
    SetupCommandsCompleted int    `json:"setup_commands_completed,omitempty"`
    SetupCommandsHash      string `json:"setup_commands_hash,omitempty"`
    Started                bool   `json:"started,omitempty"`
}
```

- `SetupCommandsCompleted` is the count of commands that have completed successfully (equivalently, the index of the next command to run). When `SetupCommandsCompleted == len(cfg.Commands)`, setup is done.
- `SetupCommandsHash` is `sha256(strings.Join(cfg.Commands, "\x00"))` rendered as hex. It is written every time progress is saved and lets the driver detect that the commands list has drifted from what the progress marker was recorded against. See the [commands-changed](#commands-changed-edge-case) section below.

`SaveConfig` (`internal/agent/config.go:88-108`) already writes atomically via `tmp + rename`, so partial saves under SIGKILL are impossible — the driver will either see the post-update file or the pre-update file. No changes required there. The same goes for `LoadConfig` (`internal/agent/config.go:72-86`); both new fields are plain JSON.

### Reworked setup loop

`internal/agent/driver.go:58-73` becomes:

```go
// Run setup commands if not already done.
if !cfg.Setup {
    hash := hashCommands(cfg.Commands)

    // If the commands list has changed since progress was last recorded,
    // restart from the top. See "commands-changed" edge case.
    if cfg.SetupCommandsHash != "" && cfg.SetupCommandsHash != hash {
        d.Log.Warn("setup commands changed, restarting from index 0",
            "previous_hash", cfg.SetupCommandsHash, "current_hash", hash,
            "previous_completed", cfg.SetupCommandsCompleted)
        cfg.SetupCommandsCompleted = 0
    }

    // Defensive clamp: if the on-disk count exceeds the current list
    // (e.g. a manual edit), start over.
    if cfg.SetupCommandsCompleted > len(cfg.Commands) {
        cfg.SetupCommandsCompleted = 0
    }

    for i := cfg.SetupCommandsCompleted; i < len(cfg.Commands); i++ {
        command := cfg.Commands[i]
        d.Log.Info("Running setup command", "index", i, "command", command)
        c := exec.CommandContext(ctx, "sh", "-c", command)
        c.Stdout = os.Stdout
        c.Stderr = os.Stderr
        if err := c.Run(); err != nil {
            return fmt.Errorf("setup command %d failed: %w", i, err)
        }
        cfg.SetupCommandsCompleted = i + 1
        cfg.SetupCommandsHash = hash
        if err := SaveConfig(d.TaskID, cfg); err != nil {
            return fmt.Errorf("failed to save config: %w", err)
        }
    }
    cfg.Setup = true
    if err := SaveConfig(d.TaskID, cfg); err != nil {
        return fmt.Errorf("failed to save config: %w", err)
    }
}
```

Key properties:

- **Save after each command, not at the end.** The original loop saved progress once after all commands; the new loop saves once per command. The atomic-rename guarantee in `SaveConfig` means the worst case under a hard kill is "we re-run the most recent command" — which is acceptable because the user-visible bug is re-running *all* commands.
- **Resume from `SetupCommandsCompleted`.** The loop ranges from the saved index to `len(cfg.Commands)`. Already-completed commands are simply not visited.
- **`cfg.Setup = true` semantics unchanged.** Only set after the last command completes; existing callers (the `setup` log line at `driver.go:54`) still mean "fully set up."
- **One final save.** Strictly redundant with the per-command save after the last command, but kept for symmetry with today's loop and to make the "fully done" transition obvious in the file (`SetupCommandsCompleted == len(Commands) && Setup == true`).

`hashCommands` is a small package-level helper:

```go
func hashCommands(cmds []string) string {
    h := sha256.New()
    for _, c := range cmds {
        h.Write([]byte(c))
        h.Write([]byte{0})
    }
    return hex.EncodeToString(h.Sum(nil))
}
```

The null-byte separator avoids the obvious collision where merging adjacent commands into one happens to produce the same `strings.Join(..., "\n")` content. `sha256` is overkill for collision resistance here (we're checking equality of small string lists, not defending against adversarial input), but it costs nothing and removes any worry about birthday-style false matches on a future longer list.

### Where progress is stored

The progress marker rides in the same per-task JSON file (`/tmp/xagent/<task-id>.json`) that already carries `cfg.Setup`. Concretely:

- `Runner.create` writes the initial cfg into the container at create time (`internal/runner/runner.go:434-437,474-475`). New containers start with `SetupCommandsCompleted: 0` and `SetupCommandsHash: ""` (zero-value, omitted from JSON by `omitempty`).
- The driver reads it with `LoadConfig` (`internal/agent/driver.go:45`) on every start, mutates it during setup, and persists with `SaveConfig` after each successful command.
- Container restarts preserve the writable layer, so the cfg.json persists exactly as today's `cfg.Setup == true` does. The workspace volume (containing the `git clone` artifact) persists alongside it on the same restart cycles.
- Container *re-create* (the `find`-miss branch in `Runner.Start`) wipes the cfg.json — but it also wipes the workspace volume only if the workspace declares no persistent mount, in which case "start setup from 0" is correct. Where the workspace volume persists across re-create (host mount), this is exactly the case the user hit; today it produces the bug, and under this design the operator's recourse — `docker rm xagent-<task-id>` — would force a clean re-create and clean re-run. That manual recovery path is unchanged; what changes is that it's no longer the *only* recovery path.

No new storage location is introduced; no migrations are required; no server-side schema changes.

### Backward compatibility

Existing on-disk configs predate the new fields. Both new fields are `omitempty`, so an older cfg.json simply has the JSON zero values when read by the new driver:

| On-disk state (old cfg.json) | New driver interprets as | Result |
|---|---|---|
| `Setup: true` | Fully set up | Skip loop entirely (line 59 `if !cfg.Setup` short-circuits). Identical to today. |
| `Setup: false`, no progress fields | `SetupCommandsCompleted: 0`, `SetupCommandsHash: ""` | Start setup from index 0. Identical to today. |
| `Setup: false` + progress fields (new container) | Resume from saved index | Resumes correctly. |

The `SetupCommandsHash != ""` guard in the reworked loop is what makes case 2 safe: a missing hash is treated as "no recorded progress to validate against" rather than "hash mismatch → reset," so we don't trip the reset path when reading a pre-existing cfg with `SetupCommandsCompleted == 0`. Case 1 is the most important one — already-setup tasks must not re-run setup just because the binary has been upgraded; the leading `if !cfg.Setup` guarantees that.

No code-level migration step is needed.

### Commands-changed edge case

This is the principal correctness risk and the reason for tracking `SetupCommandsHash`.

Today, `cfg.Commands` is effectively immutable for the lifetime of a container: `Runner.create` builds the cfg from `ws.AgentConfig()` (`internal/runner/runner.go:417`) and writes it once; subsequent `Runner.Start` calls hit the `ok == true` branch (`runner.go:489`) and never rewrite the cfg. So an in-place commands change cannot happen with today's code paths.

But the design must not silently corrupt setup state if that invariant ever relaxes. Two plausible future changes would break it:

1. The runner is taught to refresh `cfg.Commands` on restart from the current `workspaces.yaml` (e.g. to pick up edits without forcing a container rebuild).
2. A future workspace-update flow rewrites the cfg.json from outside the driver.

Both would leave the on-disk `SetupCommandsCompleted` pointing into a list whose contents at those indices have changed. Without a guard, we'd happily skip commands the user just added and run completed-looking commands that are actually new.

The recommended guard is the hash field:

- Compute `hashCommands(cfg.Commands)` at the top of the loop.
- If `cfg.SetupCommandsHash != ""` (i.e. progress has been recorded at least once) and it doesn't match the current hash, log a warning and reset `SetupCommandsCompleted` to 0.
- After every successful command, persist the *current* hash alongside the new count, so the marker always reflects the list it was recorded against.

This is coarse — any change to any element of the list resets the entire setup — but coarse is the right call for setup commands: the list is typically a handful of entries, the cost of re-running from 0 is bounded and visible, and a finer-grained scheme (see [Trade-offs](#trade-offs)) buys very little for the realistic edit patterns (append-a-command) while adding complexity.

### Logging

- `d.Log.Info("Running setup command", "index", i, "command", command)` — adds `index` so the resume point is visible in logs without grepping for command text.
- `d.Log.Info("loaded config", ..., "setup_completed", cfg.SetupCommandsCompleted)` — extend the existing `loaded config` log line at `driver.go:50-56` to include the resume index.
- `d.Log.Warn("setup commands changed, restarting from index 0", ...)` — exclusively on hash mismatch, so a one-line audit trail exists when this triggers.
- No new metrics. The driver doesn't currently emit metrics for setup; if we ever want to alert on "setup keeps restarting," that's a separate follow-up.

### Tests

The current driver tests (`internal/agent/driver_test.go`) only cover `cfg.prompt()`. The reworked loop benefits from a small extraction so it is testable without booting a container — e.g. a method `(*Driver).runSetup(ctx context.Context, cfg *Config) error` (or a free function `runSetupCommands(ctx, log, taskID, cfg) error`), called from `Driver.Run`. With that in place, the test plan is:

1. **Mid-list failure leaves the right resume index.** Configure `Commands: ["true", "true", "false", "true"]`; run the loop with a temp `ConfigDir`. Assert it returns an error from command index 2, the on-disk cfg has `SetupCommandsCompleted == 2` and `Setup == false`, and `SetupCommandsHash` matches the hash of the input list.

2. **Restart resumes from the saved index.** Seed a cfg with `SetupCommandsCompleted: 2`, `SetupCommandsHash: hashCommands(cmds)`, and `Commands: ["fail-if-run-1", "fail-if-run-2", "touch /tmp/marker3", "touch /tmp/marker4"]`. Run the loop. Assert commands 0 and 1 are not executed (no error from "fail-if-run-*"), commands 2 and 3 are, the final cfg has `Setup == true` and `SetupCommandsCompleted == 4`.

3. **Commands-changed case resets correctly.** Seed a cfg with `SetupCommandsCompleted: 2`, `SetupCommandsHash: "stale"`, `Commands: [...new list...]`. Run with all commands being `true`. Assert the loop ran every command from index 0 (e.g. via a script that increments a counter file), and the final hash matches the new list.

4. **Backward-compat: `cfg.Setup == true`.** Seed a cfg with `Setup: true` and no other progress fields; assert the loop body is skipped entirely (no commands executed even if `Commands` contains a failing command).

5. **Backward-compat: `cfg.Setup == false`, no progress fields.** Seed a cfg with `Setup: false`, `SetupCommandsCompleted: 0`, `SetupCommandsHash: ""`; assert the loop runs every command from index 0 and the hash-mismatch warning is **not** logged (since there was no prior hash to mismatch).

6. **Last-command-failure does not flip `Setup`.** `Commands: ["true", "false"]`; assert error returned, on-disk cfg has `SetupCommandsCompleted == 1`, `Setup == false`.

7. **Clamp on out-of-range index.** Seed `SetupCommandsCompleted: 99`, `Commands: ["true"]`; assert the loop runs from 0 (defensive clamp), no panic.

Tests 1, 2, 3, 6, 7 cover the new behaviour; 4 and 5 lock down backward-compat. A `setup_test.go` colocated with the extracted helper is the natural home.

## Trade-offs

| Approach | Pros | Cons |
|---|---|---|
| **Index + hash (recommended)** | Minimal schema change (two scalar fields); O(1) drift check; matches the issue's suggested shape; backward-compat is trivial via `omitempty` zero values | Any change to the commands list resets the whole sequence — an appended command would re-run everything |
| **List of completed command strings** (`SetupCompleted []string`) | Naturally handles "append a new step" — resume index is the longest prefix of `Commands` matching `SetupCompleted` | More on-disk state (full command text duplicated); resume logic is a prefix walk rather than a single integer comparison; doesn't strictly need a hash but requires deciding semantics for partial-prefix matches that aren't a clean prefix |
| **Count only (no drift detection)** | Smallest change; correct under today's invariant (`cfg.Commands` is immutable per container) | One future change to refresh `cfg.Commands` on restart silently corrupts setup state. No defence in depth. |
| **Make every setup command idempotent** | No agent-side change at all | The user has to rewrite every setup script to handle "already done" (e.g. `git clone || true`). Some commands cannot be made cleanly idempotent. Doesn't fix the *symptom* (re-running every command wastes minutes per restart). Punts the cost onto every workspace author. |

The recommended index-plus-hash approach matches the issue's suggested shape and is the minimum that gives defence in depth. Upgrading later to the list-of-strings variant is a non-breaking change (the count field is a subset of the information), so this proposal does not foreclose that direction.

## Open Questions

1. **Should we expose the resume index over the API / web UI?** The web UI surfaces task state via the existing `Task`/log streams, not via the agent cfg.json (which lives inside the container). The setup loop's progress is already visible through the `Running setup command` log lines; lifting the resume index out of the container would require a server-side schema change and is out of scope for this proposal. Easy follow-up if useful.

2. **Should `cfg.Setup = true` write `SetupCommandsCompleted = len(cfg.Commands)` as well?** Strictly redundant — once `Setup` is true, the loop is skipped — but it would make the "fully done" state self-consistent in the file. The reworked loop does this naturally (the per-command save before the final `Setup = true` save already records the full count). Calling it out so reviewers don't object that the redundant write is wasteful.

3. **Per-command timeout / max retry?** Out of scope. The current loop has no per-command timeout (it inherits the driver `ctx`); the resume design works independently of any future timeout/retry policy. The trigger in issue #751 was a transient GitHub-rate-limit failure that would have recovered on its own with a manual restart — once resume is in place, the manual restart is enough.

4. **Should hash include the working directory / shell?** The driver always runs each command as `sh -c <command>` (`driver.go:62`), so the shell is fixed. The cwd in turn is the driver's cwd (not per-command), which doesn't change across restarts of the same container. Hashing the commands list alone is sufficient unless a future workspace surface lets a workspace author change the cwd or shell — then this hash would need to expand. Worth a comment in the helper.
