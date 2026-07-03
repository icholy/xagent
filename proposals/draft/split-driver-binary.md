# Split the Driver into Its Own Binary

Issue: https://github.com/icholy/xagent/issues/1150

## Problem

The runner injects the **entire `xagent` binary** into every task sandbox as the
driver. The Docker backend reads the currently running executable
(`prebuilt.ReadBinary` → `os.Executable()` when on the matching linux/arch) and
writes it to `backend.BinaryPath` (`/usr/local/bin/xagent`) in each container
(`internal/runner/backend/docker/docker.go`). Other backends bake the same full
binary into their images (e.g. the Lambda MicroVM Dockerfile `COPY xagent
/usr/local/bin/xagent`).

Inside a sandbox, only a small slice of the binary is ever executed:

- `xagent driver` — the sandbox entrypoint (`Spec.Cmd`, `internal/runner/runner.go`)
- `xagent tool agent-mcp` — the injected `xagent` MCP server
- `xagent tool git-credential` — git credential helper for GitHub App tokens
- `xagent tool github-mcp` — GitHub MCP server
- `xagent tool microvm-shim` — Lambda MicroVM in-VM entrypoint

Everything else — `server` (with the embedded React web UI), `runner`, the
Docker client, the AWS SDK, `go-github`, Jira/Atlassian, and the `task` /
`containers` / `prune` / `logs` / `download` CLI — is **host-side only**. None of
it runs in a sandbox, yet it is shipped into every one.

Concrete cost, measured on this repo (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`):

| Binary | Size |
|---|---|
| Full `./cmd/xagent` (empty webui placeholder; the real web UI is larger) | **~58.6 MB** |
| Driver wired through the monolithic `internal/command` package | ~38.7 MB |
| Driver built against `internal/agent` directly | **~23.0 MB** |

`internal/agent` is provably clean of the AWS SDK, the Docker client, and
`go-github` (`go list -deps ./internal/agent` returns zero of each). Two things
inflate the injected binary:

1. **The embedded web UI and full server/runner/backend tree** are linked
   because `./cmd/xagent` wires every subcommand.
2. **`internal/command` is a single package.** Its package-level
   `var …Command = &cli.Command{…}` initializers reference server, runner,
   Docker, and AWS code, so importing *any* command (even just `DriverCommand`)
   links *all* of them. A naive `cmd/xagent-driver` that imports
   `command.DriverCommand` still weighs ~38.7 MB for exactly this reason.

This ~59 MB blob is written into the container filesystem on every task launch
and copied around by every backend, and it puts the entire server surface (auth,
DB access code, web UI) inside every untrusted agent sandbox.

## Design

Produce a **separate `xagent-driver` binary** that contains only the
in-container command surface, and have every backend provision *it* instead of
the full `xagent` binary.

### New entrypoint: `cmd/xagent-driver`

Add `cmd/xagent-driver/main.go` that wires only the sandbox subcommands:

```go
func main() {
	cmd := &cli.Command{
		Name:  "xagent-driver",
		Usage: "In-sandbox xagent driver",
		Commands: []*cli.Command{
			drivercmd.DriverCommand,
			drivercmd.ToolCommand, // agent-mcp, git-credential, github-mcp, microvm-shim
			drivercmd.VersionCommand,
		},
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
}
```

The command definitions themselves are unchanged in behavior; the CLI surface
(`driver`, `tool …`) stays identical so `Spec.Cmd` and the injected MCP server
config keep working verbatim.

### Split `internal/command` so the driver package is self-contained

The dead-code-elimination problem is structural: today all commands live in one
package, so linking one links the initializers of all. Move the in-container
commands into their own package (proposed `internal/command/drivercmd`, or a new
top-level `internal/drivercmd`) that imports **only** `internal/agent`,
`internal/xagentclient`, `internal/gitcredential`, `internal/githubmcp`,
`internal/microvmshim`, and `internal/version`:

- `driver.go` → `DriverCommand`
- `tool.go` → `ToolCommand`
- `agent_mcp.go` → `AgentMcpCommand`
- `git_credential.go` → `GitCredentialCommand`
- `github_mcp.go` → `GitHubMCPCommand`
- `microvm_shim.go` → `MicrovmShimCommand`
- a slim `version.go` → `VersionCommand`

The host-side `xagent` binary (`cmd/xagent`) imports both `internal/command`
(server, runner, task, containers, …) and `internal/command/drivercmd`, so its
CLI is unchanged and it still exposes `xagent driver` / `xagent tool …` for local
development and single-binary use. The key invariant is that `cmd/xagent-driver`
imports **only** `drivercmd`, never `internal/command`, so the linker keeps the
server/runner/backend/AWS/Docker/web-UI trees out.

A CI guard keeps the split from regressing — a test asserts that the driver
binary's dependency graph excludes the heavyweight modules:

```go
func TestDriverBinaryStaysSlim(t *testing.T) {
	deps := goListDeps(t, "./cmd/xagent-driver")
	for _, banned := range []string{
		"github.com/aws/aws-sdk-go-v2",
		"github.com/docker/docker",
		"github.com/google/go-github",
		"github.com/icholy/xagent/internal/server",
		"github.com/icholy/xagent/internal/runner",
	} {
		if slices.ContainsFunc(deps, func(d string) bool { return strings.HasPrefix(d, banned) }) {
			t.Errorf("driver binary must not depend on %s", banned)
		}
	}
}
```

#### `microvm-shim` and the AWS SDK

`xagent tool microvm-shim` imports `internal/microvmshim` and
`internal/x/awsmicrovm`, which pulls the AWS SDK (~66 packages, ~16 MB). It is
in-container (it is the Lambda MicroVM image entrypoint), so it legitimately
belongs to the driver surface — but it is only relevant to one backend.

Recommended split: keep `microvm-shim` **out** of the default `xagent-driver`
binary and give the Lambda MicroVM backend its own thin entrypoint
(`cmd/xagent-microvm-shim`, or a build tag) baked into that backend's image. The
common Docker/Firecracker path then gets the ~23 MB driver with no AWS SDK,
while the Lambda image carries the shim it actually needs. If keeping a single
in-container binary is preferred, `microvm-shim` can be folded back in at the
cost of the AWS SDK weight (~38.7 MB total); this is the primary open question
below.

### Provisioning: backends inject the driver binary

`backend.BinaryPath` stays `/usr/local/bin/xagent` so `Spec.Cmd` and the MCP
server config are untouched. What changes is the *source* of the bytes.

`internal/runner/prebuilt` currently keys on `xagent-linux-<arch>` and, as a
local-dev convenience, returns `os.Executable()` when running on the matching
linux/arch. That convenience breaks once the driver is a separate artifact
(the running executable is the full `xagent`, not the driver). Update the
package:

- `BinaryNames` / `BinaryPath` → `xagent-driver-linux-<arch>`.
- Drop the `os.Executable()` shortcut (or gate it behind an explicit
  `XAGENT_DRIVER_BIN` override for hacking on the driver). Local dev builds the
  driver into the prebuilt dir instead — see build changes below.
- `Download` fetches the `xagent-driver-linux-<arch>` release assets.

The Docker backend keeps writing `prebuilt.ReadBinary(arch)` to
`backend.BinaryPath`; it just gets the ~23 MB driver now. The Lambda MicroVM
Dockerfile copies `xagent-driver` (or `xagent-microvm-shim` per the split above)
instead of `xagent`.

### Build & release changes

`mise.toml` `build` task additionally builds the driver for both arches into
`prebuilt/`, so local runners find it exactly where `prebuilt.Dir()` looks:

```toml
[tasks.build]
depends = ["build:webui"]
run = [
  "CGO_ENABLED=0 go build -o xagent ./cmd/xagent",
  "mkdir -p prebuilt",
  "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o prebuilt/xagent-driver-linux-amd64 ./cmd/xagent-driver",
  "CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o prebuilt/xagent-driver-linux-arm64 ./cmd/xagent-driver",
]
```

`.github/workflows/release.yml` builds and uploads `xagent-driver-linux-amd64`
and `xagent-driver-linux-arm64` as release assets (with the same
`-ldflags "-X …/internal/version.Version=<tag>"` so `xagent-driver version`
reports the release version). These are what `xagent download` and the runner's
`prebuilt.ReadBinary` consume. The full `xagent` binary continues to ship as it
does today for server/runner hosts.

The server-side `download` command and any image build that previously relied on
the full binary being present as the driver switch to the driver asset.

## Trade-offs

- **Two binaries instead of one.** The build and release pipeline grows two more
  artifacts and the runner must locate the driver separately from its own
  executable. In exchange the injected payload drops ~61% (~59 MB → ~23 MB), the
  server/web-UI/DB code leaves every sandbox, and provisioning gets cheaper on
  every backend.

- **Package split churn.** Moving the in-container commands into their own
  package touches several files under `internal/command`, but it is mechanical
  and is the only reliable way to get the linker to drop the heavy trees — a
  driver `main` that imports the existing `internal/command` still links
  everything (measured at ~38.7 MB). The CI dependency-graph guard prevents
  regressions.

- **Loss of the `os.Executable()` dev shortcut.** Today a locally built linux
  `xagent` can act as its own driver with no `xagent download`. After the split,
  local dev must build the driver into the prebuilt dir (folded into
  `mise run build`) or set `XAGENT_DRIVER_BIN`. Minor, and `mise run build`
  already runs before tests per `CLAUDE.md`.

### Alternatives considered

- **Trim the full binary instead of splitting it** (e.g. build-tag the web UI
  out for the injected copy). This does not help: the injected binary is the
  *server's own running executable* via `os.Executable()`, which necessarily
  includes the web UI and full server. Producing a trimmed variant is
  effectively building a second binary anyway — so build the right one.

- **UPX / compression of the current binary.** Shrinks bytes-at-rest but keeps
  the entire server surface inside every sandbox, adds a decompression step, and
  is fragile across platforms. Splitting addresses the footprint and the
  security surface together.

- **A single in-container binary that keeps `microvm-shim`** (and therefore the
  AWS SDK). Simpler artifact set, but pays ~16 MB of AWS SDK on the common
  Docker path that never uses it (~38.7 MB vs ~23 MB). See open questions.

## Open Questions

- **Naming/placement of the driver command package** — `internal/command/drivercmd`
  vs a new top-level `internal/drivercmd`. Either works; the invariant is that
  `cmd/xagent-driver` never imports the heavy `internal/command`.

- **Where does `microvm-shim` live?** Keep it out of `xagent-driver` and give the
  Lambda MicroVM backend its own `xagent-microvm-shim` entrypoint (keeps the
  common driver AWS-free at ~23 MB), or fold it into a single ~38.7 MB
  in-container binary for artifact simplicity? Recommendation: split it out, but
  this depends on how much the Lambda backend is exercised.

- **Should the host `xagent` binary keep exposing `driver` / `tool …`?** Keeping
  them (by having `cmd/xagent` also import `drivercmd`) preserves the current
  single-binary UX for local runs at no extra size cost to the *injected* driver.
  Proposed: yes, keep them.
