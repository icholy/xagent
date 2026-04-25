# Fix `xagent shell` command

Issue: https://github.com/icholy/xagent/issues/488

## Problem

The `xagent shell` command has several issues that make it unreliable for debugging task containers:

1. The `--commit` flow for stopped containers creates a temporary container that doesn't preserve the original container's environment variables, networking, or user. The resulting shell environment doesn't match what the agent ran in.
2. No input validation on task ID — used as a raw string, unlike the rest of the codebase which parses as int64.
3. Hardcoded `/bin/sh` with no way to specify an alternative shell.
4. No `--user` flag — always enters as root regardless of what user the container was configured with.

## Design

### Input Validation

Parse the task ID as int64 and use `fmt.Sprintf("xagent.task=%d", taskID)` for the label filter, matching the pattern used in `runner.go:find()`:

```go
taskID, err := strconv.ParseInt(cmd.Args().First(), 10, 64)
if err != nil {
    return cli.Exit("invalid task ID: "+cmd.Args().First(), 1)
}
// ...
Filters: filters.NewArgs(filters.Arg("label", fmt.Sprintf("xagent.task=%d", taskID))),
```

### New Flags

Add `--user` and `--shell` flags:

```go
Flags: []cli.Flag{
    &cli.BoolFlag{
        Name:  "commit",
        Usage: "For stopped containers, commit the filesystem to a temporary image and run a shell in it",
    },
    &cli.StringFlag{
        Name:  "user",
        Aliases: []string{"u"},
        Usage: "User to run the shell as (defaults to the container's configured user)",
    },
    &cli.StringFlag{
        Name:  "shell",
        Usage: "Shell to use (default: /bin/sh)",
        Value: "/bin/sh",
    },
},
```

### Running Container Path

Pass the user and shell flags through to `docker exec`:

```go
if c.State == "running" {
    args := []string{"exec", "-it"}
    if user := cmd.String("user"); user != "" {
        args = append(args, "-u", user)
    }
    args = append(args, c.ID[:12], cmd.String("shell"))

    dockerCmd := exec.CommandContext(ctx, "docker", args...)
    dockerCmd.Stdin = os.Stdin
    dockerCmd.Stdout = os.Stdout
    dockerCmd.Stderr = os.Stderr
    return dockerCmd.Run()
}
```

### Stopped Container `--commit` Path

Preserve the original container's environment variables, user, and networking config when creating the temporary container:

```go
inspect, err := docker.ContainerInspect(ctx, c.ID)
if err != nil {
    return fmt.Errorf("failed to inspect container: %w", err)
}

args := []string{"run", "-it", "--rm"}

// Preserve binds
for _, b := range inspect.HostConfig.Binds {
    args = append(args, "-v", b)
}

// Preserve environment variables
for _, e := range inspect.Config.Env {
    args = append(args, "-e", e)
}

// Preserve working directory
if inspect.Config.WorkingDir != "" {
    args = append(args, "-w", inspect.Config.WorkingDir)
}

// Preserve user (flag overrides original)
user := cmd.String("user")
if user == "" {
    user = inspect.Config.User
}
if user != "" {
    args = append(args, "-u", user)
}

// Preserve network mode
if inspect.HostConfig.NetworkMode != "" {
    args = append(args, "--network", string(inspect.HostConfig.NetworkMode))
}

args = append(args, tmpImage, cmd.String("shell"))
```

This ensures the temporary shell environment closely matches what the agent actually ran in, making it useful for reproducing and debugging issues.

### Error Messages

Improve error messages to suggest troubleshooting:

```go
if len(containers) == 0 {
    return cli.Exit(fmt.Sprintf("no container found for task %d (is the runner active? check 'xagent containers')", taskID), 1)
}
```

## Trade-offs

### Preserving all container config vs. minimal config

The proposal preserves env vars, user, working directory, binds, and network mode. It does **not** preserve:
- The original container's command (replaced with the shell)
- Labels (not needed for a temporary debug container)
- The proxy socket bind mount (the xagent socket likely isn't valid for a stopped task)

This is a pragmatic middle ground. Preserving everything would require re-creating the exact container config, which is complex and unnecessary for debugging. The selected fields cover the most common reasons a shell environment feels "wrong."

### `--shell` flag vs. auto-detection

Auto-detecting the available shell (checking for bash, then zsh, then sh) was considered but adds complexity and requires exec'ing into the container first. A simple `--shell` flag with `/bin/sh` as the default is more predictable.

### Docker CLI vs. Docker API

The implementation continues using `os/exec` to call the `docker` CLI rather than the Docker API directly. This is intentional — `docker exec -it` and `docker run -it` handle TTY allocation, signal forwarding, and terminal resize automatically. Replicating this with the Docker API would be significantly more complex for no real benefit.

## Open Questions

1. **Should `--commit` be the default for stopped containers?** Currently it requires an explicit flag. Since users typically want to debug a stopped container's filesystem, making it the default (with `--no-commit` to opt out) might be more ergonomic.

2. **Should the proxy socket be preserved?** If the runner is still active and the proxy socket exists on the host, preserving the bind mount would allow the xagent MCP tools to work inside the debug shell. This could be useful but might also cause confusion if the task state has changed.
