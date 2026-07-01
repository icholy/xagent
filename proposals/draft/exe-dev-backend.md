# exe.dev Backend for the Runner

Issue: https://github.com/icholy/xagent/issues/1107

## Problem

The runner's sandbox runtime is abstracted behind `backend.Backend`
(proposals/accepted/runner-backend-interface.md). Docker is the shipping
implementation, a `lambda-microvm` backend runs AWS-managed Firecracker microVMs
(proposals/draft/lambda-microvm-backend.md), and Firecracker + AgentCore are
proposed. Every managed option so far is **AWS**, and the two most managed ones
(Lambda MicroVMs, AgentCore) require **purpose-built images** — a snapshot or an
`/invocations`-server image built ahead of time — which is a real regression in
`workspaces.yaml` portability versus Docker, which consumes an unmodified image.

[exe.dev](https://exe.dev/) is a managed cloud-VM platform. It provisions
per-second-billed **sandbox** VMs (and persistent VPS/devbox VMs) that boot full
Linux with **root, SSH, `apt`, and `systemd`** on AWS-independent infrastructure.
Like the AWS managed backends there is no host hypervisor, kernel, or networking
for the runner to operate; unlike them, VMs run **stock images** (`exeuntu`,
`alpine`, …) — no purpose-built snapshot — and are reached over ordinary SSH plus
a thin HTTPS `/exec` API rather than an AWS proxy.

This proposal adds an `exe-dev` backend implementing `backend.Backend`: a
managed, no-host-compute runtime that is **not** AWS-coupled and keeps Docker's
unmodified-image portability. It also exercises a different corner of the
interface than the AWS backends: exe.dev exposes **no lifecycle push stream**, so
`Wait` is **poll-based** — which the interface explicitly allows (`Probe` and a
returning `Wait` were shaped "so poll-based backends fit as naturally as
stream-based ones").

## Background: the exe.dev contract

Several facts about exe.dev drive the design. exe.dev's programmatic surface is
deliberately thin ("the HTTPS API is the SSH API"), and that thinness shapes
every method below.

1. **A sandbox is a full Linux VM you create by name.** The REPL/CLI command
   `new <name> --image <image>` provisions a VM from a stock base image; the npm
   SDK mirrors it as `exe.new(name, { image })`. Images include `exeuntu`
   (Ubuntu) and `alpine`. The VM boots with **root, SSH, `apt`, and `systemd`** —
   an ordinary machine, not a snapshot restored around one process. A sandbox
   persists (billing per second while running) until it is explicitly removed.

2. **The HTTPS API is SSH-in-a-POST-body.** `POST https://exe.dev/exec` with
   `Authorization: Bearer <token>` runs the request body "exactly as if it were
   typed into the REPL or exec'd via ssh," always returning JSON. Its limits are
   load-bearing for the design: **no stdin, no pty, a 30-second timeout** (HTTP
   504 past it), and a **64 KB request-body cap**. So `/exec` is fine for control
   commands (`new`, `rm`, `ls`, `systemctl …`) and short in-VM queries, but it
   **cannot** stream, hold a long-running process, or carry a multi-megabyte
   driver binary.

3. **Bearer tokens are SSH-key-signed and mintable offline.** An `exe1.…` token
   is produced by signing with your exe.dev **SSH private key**, entirely locally
   and programmatically. The runner therefore holds exactly **one credential** —
   the exe.dev SSH key — and gets two uses from it: minting short-lived `/exec`
   bearer tokens, and authenticating **SSH/SFTP** sessions into the VM.

4. **File transfer and long-lived processes go over SSH, not `/exec`.** Because
   `/exec` caps the body at 64 KB and cannot hold a process, the driver binary
   (tens of MB) and a supervised long-running driver are handled over an SSH
   channel: **SFTP** uploads files, and `systemd-run` launches the driver as a
   detached transient unit whose exit status systemd records. This is the exe.dev
   analog of the Docker backend's `CopyToContainer` tar + supervised container.

5. **The driver only needs egress.** exe.dev VMs have outbound internet, so the
   driver connects **out** to the C2 by URL + task token exactly as under Docker —
   no inbound proxy, no ingress connector. The runner→VM direction (provision,
   launch, probe, stop) travels over SSH/`/exec`; the VM→C2 direction is a plain
   egress connection. This is strictly simpler than Lambda's managed-proxy SSE
   plumbing, which existed only to carry a completion signal the runner here
   obtains by polling.

6. **There is no lifecycle push stream, and the programmatic list/destroy
   surface is thin.** exe.dev does not expose an SSE/webhook that fires when an
   in-VM process exits, and it does not publish a first-class "list/destroy VMs"
   API — the REPL `ls`/`rm` commands (over `/exec`, JSON output) are the de-facto
   surface. So the backend **cannot** be notified of completion; it must
   **poll**. Correctness does not depend on prompt notification (the
   driver-owned-events invariant means the driver reports its terminal status to
   the C2 directly — see below); poll latency only bounds how quickly the runner
   reaps or stops a finished VM.

The structural fit with `backend.Backend`: an exe.dev VM is a runner-independent
sandbox we *launch and observe* — like a Docker container or a Lambda microVM,
**not** a request we hold open (AgentCore). It survives a runner restart (re-adopt
by name/id) and accepts a graceful in-VM SIGTERM. The one place it diverges from
the AWS backends is the completion signal: poll, not push.

## Design

### Overview

A new package `internal/runner/backend/exedev` implements `backend.Backend`. It
reaches the service through `internal/x/exe`, a general-purpose exe.dev client:
SSH-key token signing, an `/exec` HTTPS caller (bearer auth, JSON responses), and
SSH/SFTP helpers built on `golang.org/x/crypto/ssh` + `github.com/pkg/sftp`.
Selection follows the existing seam in `internal/command/runner.go`:

```
xagent runner --backend exe-dev
```

Per task, on a fresh launch the backend:

1. Creates a VM via `/exec` REPL `new xagent-<runner>-<taskID> --image <image>`
   (a deterministic, runner-namespaced name; see Handle). The JSON response
   carries the VM's id and SSH address.
2. Waits for SSH reachability (dial with backoff).
3. Over **SFTP**, uploads the driver binary to `backend.BinaryPath`
   (`/usr/local/bin/xagent`, mode 0755) and `spec.Files` (the agent config) to
   their absolute paths.
4. Launches the driver **detached** as a systemd transient unit over SSH:
   `systemd-run --unit=xagent-driver --collect --setenv=XAGENT_TASK_ID=… … <cmd>`,
   with `spec.Cmd` (`xagent driver --server … --task … --token …`), `spec.Env`
   (`XAGENT_TASK_ID` / `XAGENT_TOKEN` / `XAGENT_SERVER`), and the workspace
   environment. The driver connects to the C2 with its task token exactly as
   under Docker.

The driver binary is sourced from `prebuilt.ReadBinary(arch)` for the VM's
architecture (amd64 for `exeuntu`), reusing the exact mechanism the Docker
backend uses to inject the binary. The orchestrator (`runner.Runner`), the
driver, the C2 API, the database, and the task state machine are **untouched**:
the driver already connects to the C2 by URL + token and neither knows nor cares
what launched it. That was the point of the socket-proxy elimination and
driver-owned-events prerequisites.

### Why `systemd-run` for the driver

`/exec` cannot hold the driver (30 s timeout, no pty), so the driver must run
detached and its exit status must be recoverable **later**, by a possibly
different runner process (after a restart) and without a held connection. exe.dev
VMs run `systemd`, so the backend launches the driver as a **transient unit**:

```
systemd-run --unit=xagent-driver --collect \
  --setenv=XAGENT_TASK_ID=<id> --setenv=XAGENT_TOKEN=<jwt> --setenv=XAGENT_SERVER=<url> \
  /usr/local/bin/xagent driver --server <url> --task <id> --token <jwt>
```

This gives, for free and without a wrapper script or sentinel file:

- **Detached supervision** — the unit outlives the `/exec` call that created it.
- **A poll-able exit code** — `systemctl show xagent-driver -p ActiveState,Result,ExecMainStatus`
  returns the driver's real process exit code once the unit is `inactive`/`failed`.
  This is the exe.dev analog of Docker's `ContainerWait` exit code and Lambda's
  `driver-exited{code}` SSE event.
- **A graceful-stop primitive** — `systemctl stop xagent-driver` sends SIGTERM,
  honors `TimeoutStopSec`, then SIGKILL: exactly the SIGTERM→grace→SIGKILL
  contract `Signal` needs.
- **Restart survival** — the unit is named, so a restarted runner re-attaches to
  a task's driver by re-probing the unit, no local process handle required.

### Lifecycle: stop on exit, remove on archive

The interface expects a sandbox that, on driver exit, becomes an **exited husk
preserved for reuse** (`StateExited`) and is destroyed only on task archive
(`StateGone`). Docker gets this from container semantics; Lambda gets it from
`suspend-microvm`. exe.dev's mapping depends on **whether it exposes a
stop/suspend verb that preserves the disk while halting per-second billing**
(exe.dev markets both disposable per-second sandboxes and persistent VMs, which
strongly implies a stop/start lifecycle exists; the exact verb is unconfirmed
from the public docs — see Open Questions). The design is written for the
**stop-capable** path and names the fallback:

| | Docker | Lambda MicroVM | exe.dev (primary) |
|---|---|---|---|
| driver exits | container exits (preserved, no cost) | `suspend-microvm` (preserved, no compute) | **`stop` the VM** (disk preserved, per-second billing halts) |
| next run / restart | reuse exited container | `resume-microvm` | **`start` the VM, re-launch the driver unit** |
| task archived/deleted | remove container | `terminate-microvm` | **`rm` the VM** |

On the stop-capable path the lifecycle is fully symmetric with Docker/Lambda: an
event-driven task (subscribed to a PR) completes, the VM stops (disk intact, no
compute billed), and the next routed event `start`s it and re-launches the driver
against the preserved filesystem — no re-create, no re-provision, setup markers
(`agent.ConfigPath`) intact.

**If exe.dev exposes no disk-preserving stop**, `StateExited` still models a VM
that is up but whose driver has exited — reuse simply re-launches the driver unit
— but the VM keeps billing while idle. Two mitigations, both surfaced as
trade-offs: (a) accept idle cost and lean on `Destroy`-on-archive + a max-idle
reaper; or (b) `Destroy` on driver exit, giving up 1:1 filesystem reuse (a
follow-up event re-creates a fresh VM and re-runs setup — acceptable for
non-event-driven tasks). The backend picks (a) as the default fallback so
reuse-on-event still works, matching the Docker/Lambda mental model.

### Backend method mapping

The backend implements the runtime-only interface; all task↔handle persistence is
the runner's (shared `taskstate` store, per proposals/draft/shared-runner-taskstate.md).

| Method | Implementation |
|---|---|
| `ValidateWorkspace` | Require `exe_dev.image`. (Credentials are runner-level, not per-workspace — see Config.) |
| `Launch` | **Fresh (`reuse` nil):** `new xagent-<runner>-<taskID> --image <image>` over `/exec`; wait for SSH; SFTP the `prebuilt` driver binary to `BinaryPath` + `spec.Files`; `systemd-run` the driver detached. Return the `Handle` (id = VM id, `Data` = SSH host + image). **Reuse (`reuse` non-nil):** adopt the recorded VM — `start` it if stopped, then re-launch the `xagent-driver` unit against the preserved disk (the exe.dev analog of restarting an exited container). If the VM is gone (`ls`/status not-found) return `backend.ErrGone` — never create a fresh VM on the reuse path, since a task is bound 1:1 to its sandbox. |
| `Probe` | `ls`/status for the VM over `/exec`: not-found → `StateGone`. Present + `systemctl is-active xagent-driver` = `active` → `StateRunning`. Present + inactive/failed (driver exited; on the stop-capable path, VM stopped) → `StateExited`. |
| `Signal` | `systemctl stop xagent-driver` over `/exec` (SIGTERM → `TimeoutStopSec` grace → SIGKILL). The driver catches SIGTERM and owns its terminal report to the C2; its exit then drives the stop like any other completion. Returns `signalled=true` if a running unit was reached. Does **not** `rm` — that is `Destroy`'s job. |
| `Destroy` | The only removal path, reached via `Prune` on task archive/delete: `rm xagent-<runner>-<taskID>` over `/exec`, idempotent (a not-found `rm` is not an error). |
| `Wait` | **Poll** (no push stream): loop `systemctl show xagent-driver -p ActiveState,Result,ExecMainStatus` over `/exec` on a capped backoff. Unit `inactive`/`failed` → clean completion: on the stop-capable path `stop` the VM (halt billing, preserve disk) and return `(ExecMainStatus, nil)`. VM gone (`ls` not-found) → `(ExitLost, nil)`. A transient `/exec` error (504, network) is swallowed and retried. `ctx` cancelled (runner shutting down) → `(_, ctx.Err())` with the VM left intact for next-boot rehydration; the caller must not emit "failed". |
| `Close` | Close pooled SSH connections and the HTTP client; leave VMs running/stopped — they outlive the runner, exactly as containers do today. |

**Exit-code fidelity and the driver-owned-events invariant.** `Wait` observes the
driver's **true** process exit code via `ExecMainStatus`, not VM state. Correctness
does not hinge on that code: the driver reports its terminal status **directly to
the C2** before exiting, and the runner's reconcile treats an exited sandbox whose
task is still `RUNNING` as a lost report (`failed`), regardless of code. When the
unit is gone and the VM is not-found, `Wait` returns `ExitLost` (`-1`) and the
state machine's status guard reconciles — the same fallback the AWS backends rely
on. The only cost of polling is latency to the *stop/reap*, never task-status
correctness.

### The Handle

The handle's index id (`Handle.ID`, the reverse-index key the runner resolves back
to a task) is the **exe.dev VM id** returned by `new`. `Handle.Data` carries what
the backend needs to reach the VM but not for identity:

```go
// stored opaque in Handle.Data (taskstate.Record.Data), never decoded by the store
type handleData struct {
	Host  string `json:"host"`  // VM SSH/exec address, for provisioning + control
	Image string `json:"image"` // stock base image the VM was created from
}
```

The VM is also **named** deterministically `xagent-<runnerID>-<taskID>`, so a
restarted runner re-adopts a task's VM from the `taskstate` store (primary) and,
as a backstop, by name via `ls` filtered to its runner prefix. The runner-id
prefix namespaces VMs when a fleet of runners shares one exe.dev account, avoiding
name collisions.

### Workspace config

Per the backend-interface proposal, backends get sibling config sections.
`workspace.Workspace` gains an `exe_dev:` section next to `container:` and
`lambda_microvm:`:

```yaml
workspaces:
  pets-workshop:
    exe_dev:
      image: exeuntu               # stock exe.dev base image (exeuntu, alpine, …)
      working_dir: /root           # optional
      environment:
        CLAUDE_CODE_OAUTH_TOKEN: ${env:CLAUDE_CODE_OAUTH_TOKEN}
    agent:
      type: claude
      ...
```

```go
// ExeDev holds the exe.dev backend's runtime config for a workspace. exe.dev
// credentials are NOT configured here; the exe.dev SSH key resolves from the
// runner's flags/env, so a single workspaces.yaml is safe to share across a fleet.
type ExeDev struct {
	// Image is the stock exe.dev base image to create the VM from (exeuntu, alpine, …).
	Image string `yaml:"image"`
	// WorkingDir is the driver's working directory in the VM. Optional.
	WorkingDir string `yaml:"working_dir"`
	// User is the account the driver runs as. Optional; defaults to root.
	User string `yaml:"user"`
	// Environment is injected into the driver's systemd unit.
	Environment map[string]string `yaml:"environment"`
}

func (e *ExeDev) Environ() []string { /* "k=v" slice, like Container/LambdaMicroVM */ }
```

`Workspace.ExeDev` is a pointer (`*ExeDev`, `omitempty`), mirroring `LambdaMicroVM`,
so a workspace may set `container:`, `lambda_microvm:`, and `exe_dev:` together and
one `workspaces.yaml` serves runners with different backends; each backend
validates and consumes only its own section. `agent:`, `commands:`, and
`capabilities:` stay backend-agnostic. The backend's `ValidateWorkspace` requires
`exe_dev.image`; `RegisterWorkspaces` skips (with a warning) workspaces that fail
validation, so a shared config advertises each workspace only from runners that
can run it.

### CLI

```
xagent runner --backend exe-dev \
  [--exe-dev-key ~/.ssh/exe.dev]        # exe.dev SSH private key (token signing + SFTP)
  [--exe-dev-api-url https://exe.dev]   # default https://exe.dev
  [--exe-dev-poll 5s]                   # Wait poll interval (default)
```

Each flag has an `XAGENT_EXE_DEV_*` env source, matching the `lambda-microvm`
flags. Credentials are runner-level (like AWS creds for the Lambda backend), never
in `workspaces.yaml`. `internal/command/runner.go`'s backend switch gains an
`exe-dev` case constructing `exedev.New(...)` with the signing key, API URL, poll
interval, runner id, and logger. `xagent download` is not extended — there is no
host kernel or hypervisor binary to fetch; the exe.dev client is compiled in.

### Package layout

```
internal/runner/
├── runner.go                 unchanged orchestrator (owns the taskstate store)
├── taskstate/                shared store; the runner is the only writer
├── backend/
│   ├── backend.go            unchanged interface
│   ├── docker/               unchanged
│   ├── lambdamicrovm/        unchanged
│   └── exedev/
│       └── exedev.go         exe.dev implementation
└── workspace/                +ExeDev config section
internal/x/exe/               general-purpose exe.dev client: token signing, /exec, SSH/SFTP helpers
internal/command/             +exe-dev backend case & flags
```

`internal/x/exe` is the general-purpose client (there is no official Go SDK): it
signs `exe1.` bearer tokens from the SSH key, wraps `POST /exec` (bearer auth,
64 KB/30 s aware, JSON responses), and exposes SFTP upload + `systemd-run`/`systemctl`
helpers over an SSH channel. The exe.dev calls sit behind a small interface so the
backend is unit-tested against a fake, matching the `dockerx` moq pattern.

### Testing

- Unit tests (no exe.dev): `ValidateWorkspace`; `new`/`rm`/`ls` command assembly;
  `systemd-run` command construction (cmd/env/working-dir/user); handle
  construction (id = VM id, host+image in `Data`); the poll loop in `Wait`
  (`inactive` → exit code from `ExecMainStatus`; not-found → `ExitLost`; 504/network
  → swallow and retry; `ctx` cancel → `ctx.Err()`, no exit); reuse-vs-fresh
  branching (`start` + re-launch on reuse, `ErrGone` when the VM is not-found). The
  `/exec` and SSH clients sit behind interfaces so they are mocked.
- The token-signing and `/exec` request assembly in `internal/x/exe` are unit-tested
  against a fake SSH key and an `httptest` server (bearer header, JSON decode,
  504-as-timeout mapping, 64 KB body guard).
- Integration tests in `backend/exedev`, skipped unless an exe.dev key + a test
  account are present (an env guard, mirroring how the Docker e2e tests require a
  daemon and the Lambda tests require AWS creds). They cover create → provision →
  driver run → poll `Wait` observing the real exit code, reuse re-launching the
  driver against the preserved disk, graceful stop via `Signal`, `rm` via `Destroy`,
  and re-adoption after a simulated runner restart.
- The orchestrator needs no new tests: it already runs against `BackendMock`.

### What doesn't change

The orchestrator (`runner.go`), `EventQueue`, proto definitions, database schema,
driver, and task state machine are untouched. The Docker and Lambda MicroVMs
backends are unaffected. `prebuilt` is reused as-is to source the VM-arch driver
binary uploaded over SFTP.

## Comparison with the sibling backends

| | Docker | Lambda MicroVMs | AgentCore | **exe.dev** |
|---|---|---|---|---|
| Host owns hypervisor | n/a | no | no | **no** |
| Runner-owned compute | yes | no | no | **no** |
| Cloud coupling | any Docker host | AWS | AWS | **exe.dev (non-AWS)** |
| Image | unmodified | purpose-built snapshot | purpose-built server | **unmodified (stock base)** |
| Work survives runner restart | yes (container) | yes (VM, by id) | uncertain (held request) | **yes (VM, by id/name)** |
| On driver exit | container exits (preserved, no cost) | `suspend-microvm` (preserved, no compute) | hard teardown | **`stop` (preserved, billing halts)†** |
| Reuse on next run | restart exited container | `resume-microvm` | re-invoke (fresh) | **`start` + re-launch driver unit** |
| Graceful stop (SIGTERM) | yes | yes (`/xagent/stop`) | no | **yes (`systemctl stop`)** |
| Completion signal | docker `die` event | SSE `driver-exited{code}` | held-request return | **poll `systemctl show` (`ExecMainStatus`)** |
| File injection | tar copy | S3 bundle + presigned URL | invocation payload | **SFTP over SSH** |
| Runner→sandbox transport | Docker socket | AWS managed proxy | SigV4 HTTPS | **SSH + `/exec` HTTPS (SSH-key auth)** |

† on the stop-capable path; see the lifecycle section and Open Questions for the
fallback if exe.dev exposes no disk-preserving stop.

exe.dev is the only proposed managed backend that is **not AWS-coupled** and
**consumes an unmodified image**, at the cost of a **poll**-based completion signal
(no push stream) and a thinner, less formally documented programmatic surface.

## Trade-offs

**Poll vs. push completion.** Docker gets a `die` event and Lambda an SSE
`driver-exited{code}`; exe.dev has neither, so `Wait` polls `systemctl show`. The
cost is completion **latency** bounded by the poll interval (`--exe-dev-poll`,
default 5 s) and a little `/exec` traffic per running task. The benefit is
simplicity and restart-robustness: polling re-attaches trivially after a runner
restart (re-probe the named unit), whereas a held stream must reconnect. Crucially,
poll latency never affects **task-status correctness** — the driver reports its
terminal status to the C2 directly (driver-owned events); polling only governs how
fast the runner stops/reaps a finished VM.

**`systemd-run` unit vs. a wrapper/sentinel.** The driver could be `nohup`'d with
its exit code written to a file the runner reads back over `/exec`. Using a
`systemd` transient unit instead reuses the OS's own supervision: detached
lifetime, a poll-able `ExecMainStatus`, and a SIGTERM→grace→SIGKILL `stop` for
free — no bespoke wrapper, and it works identically across `exeuntu`/`alpine`
(both ship `systemd`/OpenRC; the client abstracts the init system if `alpine` lacks
`systemd-run`, see Open Questions).

**SFTP provisioning vs. `/exec` or in-VM download.** `/exec`'s 64 KB body cannot
carry the multi-MB driver binary, so files go over SFTP on the same SSH channel the
token key already authenticates — one credential, no extra infrastructure. The
alternative (have the VM `curl` the binary from a URL the C2 serves) would add a
C2 endpoint and a public artifact surface; SFTP keeps the backend self-contained,
mirroring the Docker tar-copy.

**One SSH key on the runner vs. per-VM credentials.** The runner holds the exe.dev
SSH key and uses it for both token signing and SFTP. That single key is the trust
anchor for the whole fleet — the same shape as the runner holding the Docker socket
or the AWS credential chain. Short-lived, per-call `/exec` bearer tokens minted from
it bound the blast radius of a leaked token; the SSH key itself stays on the runner
and never enters a VM (the driver authenticates to the C2 with its **task** JWT, not
the exe.dev key).

**Stop-on-exit depends on an exe.dev capability.** The clean Docker-symmetric
lifecycle (stop on exit, start on reuse) assumes exe.dev exposes a disk-preserving
stop that halts per-second billing. exe.dev advertising both disposable per-second
sandboxes and persistent VMs strongly implies one exists, but the public docs
(JS-rendered, thin API) don't confirm the verb. The design degrades gracefully: if
there is no such stop, `StateExited` still models "VM up, driver exited," reuse
re-launches the driver, and the fallback reaper (Destroy-on-archive + max-idle)
bounds idle cost — at the price of paying for idle VMs between events. This is the
one capability the design is contingent on and the first open question.

**Non-AWS managed runtime vs. the AWS backends.** exe.dev trades AWS's mature,
SigV4-signed, well-documented control plane (IAM, quotas, SSE proxy) for a thinner
SSH/`/exec` surface — but buys AWS-independence and stock-image portability. For a
deployment that doesn't want to stand up IAM roles, network connectors, S3 staging,
and purpose-built images, exe.dev is a materially lighter managed option; for a
deployment already on AWS, `lambda-microvm` remains the richer fit.

## Open Questions

1. **Disk-preserving stop.** Does exe.dev expose a stop/suspend verb that halts
   per-second billing while preserving the VM's disk (and a matching start/resume)?
   The primary lifecycle assumes yes (Docker/Lambda-symmetric reuse). If no, the
   backend falls back to idle-billed-preserved or Destroy-on-exit (losing 1:1 reuse)
   — which default is right, and can a runner-side max-idle reaper bound the cost?
   This is the one capability the whole lifecycle hinges on.
2. **Programmatic list/destroy.** exe.dev does not publish a first-class
   list/destroy API; `Probe`, `Destroy`, and post-restart re-adoption rely on the
   REPL `ls`/`rm` commands (JSON output) over `/exec`. Are those stable and
   scriptable enough, or should the runner track VMs purely from the `taskstate`
   store and never enumerate?
3. **VM addressing & readiness.** What exactly does `new` return for the VM's SSH
   address, and how is a fresh VM detected as ready — dial SSH with backoff, or is
   there a status field? The provisioning step blocks on this.
4. **Init system across images.** `systemd-run` assumes `systemd`. `exeuntu` has it;
   does `alpine` (OpenRC) need a different launch primitive, or should the client
   normalize on a small supervisor it uploads? Keeping the driver-launch primitive
   image-agnostic is the goal.
5. **Token lifetime.** What `/exec` bearer-token expiration balances re-mint churn
   against blast radius on leak, given `Wait` re-mints per poll cycle? (Tokens are
   cheap to sign locally, so short is likely fine.)
6. **Fleet namespacing & quotas.** Deterministic `xagent-<runner>-<taskID>` names
   avoid collisions across a shared account, but what are exe.dev's per-account VM
   count / concurrency quotas, and should the orchestrator's `--concurrency`
   semaphore surface a quota-exceeded error as backpressure (as the Lambda backend
   does for `ServiceQuotaExceededException`)?
7. **Cost controls.** Sandboxes bill per second while running. Beyond stop-on-exit,
   should the backend expose a per-task max-duration backstop (as `lambda-microvm`
   does) so a hung driver on a preserved-but-running VM can't bill indefinitely?
