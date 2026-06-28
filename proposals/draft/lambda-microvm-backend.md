# AWS Lambda MicroVMs Backend for the Runner

Issue: https://github.com/icholy/xagent/issues/1048

> **Revision (per [#1081](https://github.com/icholy/xagent/issues/1081)).** This
> proposal originally had the in-VM shim **self-terminate** its own MicroVM when
> the driver exited, using `lambda-microvms:TerminateMicrovm` granted to the
> (untrusted) agent's execution role. The investigation in #1081 verified two
> facts that reshape the lifecycle: a MicroVM does **not** stop or stop billing
> when its process exits (so explicit termination is mandatory), and IAM **cannot**
> scope `TerminateMicrovm` to "this VM only" (so a self-terminating guest can
> terminate *sibling tasks'* VMs — a cross-task DoS). The design below moves
> termination to the **runner** (which is trusted and already holds the
> credentials) and has the in-VM shim **notify** the runner of driver completion
> over an **SSE stream through AWS's managed auth-token proxy**, with the control
> plane as the liveness authority. Implementation follows in
> [#1054](https://github.com/icholy/xagent/pull/1054).

## Problem

The runner's sandbox runtime is abstracted behind `backend.Backend`
(proposals/accepted/runner-backend-interface.md), but Docker is still the only
implementation. Two isolated/managed alternatives are already proposed, and each
pays a price:

- **Firecracker** (proposals/draft/firecracker-backend.md) runs per-task KVM
  microVMs, but the runner host owns the whole virtualization stack: `/dev/kvm`,
  a guest kernel, TAP/bridge/NAT, rootfs conversion, base-image GC, the jailer.
- **Bedrock AgentCore** (proposals/draft/agent-core-backend.md) is fully managed
  with no host compute, but it is *connection-coupled* — work is driven by a held
  `InvokeAgentRuntime` request, so a runner restart risks cancelling the session
  (its central open question), and `Stop` is a hard teardown with no in-microVM
  SIGTERM.

[AWS Lambda MicroVMs](https://docs.aws.amazon.com/lambda/latest/dg/lambda-microvms-guide.html)
sit between the two. They are **AWS-managed Firecracker** — the same hypervisor
the Firecracker backend would run by hand — with no host kernel, KVM, or
networking to manage. But unlike AgentCore, microVMs are **launched
autonomously**: `run-microvm` returns a `microvmId` + `endpoint` and the VM keeps
running independently of the caller, and AWS exposes **lifecycle hooks** (`/run`,
`/suspend`, `/resume`, `/terminate`) that let us deliver a graceful in-microVM
stop. So a task's sandbox **survives a runner restart** (re-adopted by id, like a
container that outlives the runner) and a graceful stop can SIGTERM the driver —
closing the two biggest gaps in the AgentCore design — while keeping AgentCore's
"no host to run" property.

This proposal adds a `lambda-microvm` backend implementing `backend.Backend`:
AWS-managed, hardware-isolated microVMs per task with no runner-owned compute,
hypervisor, or scheduler.

## Background: the Lambda MicroVMs contract

Several facts about Lambda MicroVMs drive the design.

1. **A MicroVM image is a control-plane resource built ahead of time.** You
   package application code + a `Dockerfile` into a zip, upload to S3, and call
   `create-microvm-image`. Lambda runs the `Dockerfile`, starts your application,
   and snapshots the initialized environment. The result is a MicroVM **image
   ARN** you launch from. This is not per-task — it is built once per workspace
   and reused, analogous to AgentCore's agent runtime resource and Firecracker's
   cached base rootfs.

2. **Work starts by launching a VM, not by holding a request.** `run-microvm`
   `--image-identifier <arn>` provisions a fresh microVM from the snapshot,
   returns `{microvmId, endpoint}`, and the VM **runs autonomously** until
   suspended or terminated. There is no caller-held connection that keeps the work
   alive — the decisive difference from AgentCore. (The runner *does* hold an SSE
   notification stream to the VM, but that stream only observes the VM; dropping
   it does not stop the work, exactly as closing `docker logs` does not stop a
   container.)

3. **A MicroVM keeps running — and billing — until explicitly terminated.**
   Verified in #1081 against the AWS state table: `RUNNING → TERMINATING` happens
   **only** on an explicit `terminate-microvm` call or when
   `maximumDurationInSeconds` is exceeded. *Nothing* transitions a VM out of
   `RUNNING` because its entrypoint/driver process exited. Running VMs incur
   compute charges; terminated VMs incur none. So **explicit termination is
   mandatory** — a "shim that just exits when the driver finishes" is off the
   table: it would leak a billed `RUNNING` VM (with a dead application behind a
   dead endpoint) until `max_duration`.

4. **Per-VM config is delivered via the run hook payload.** `run-microvm`
   `--run-hook-payload` accepts a **≤16 KB string** that Lambda delivers (with the
   injected `microvmId`) to the application's `POST /aws/lambda-microvms/runtime/v1/run`
   hook. Traffic to the endpoint begins only after `/run` returns HTTP 200.

5. **Lifecycle is run / suspend / resume / terminate, with hooks.** The
   application exposes HTTP hooks Lambda calls at each transition. The
   `/terminate` hook runs *before* resources are released — our seam for a
   graceful SIGTERM. `--maximum-duration-in-seconds` (≤28,800 = 8 h) caps total
   lifetime; `list-microvms` enumerates VMs (filterable by image).

6. **A running VM is reachable over a managed proxy with no self-managed
   ingress.** Mint a short-lived token with
   `CreateMicrovmAuthToken(microvmIdentifier, expirationInMinutes, allowedPorts)`,
   then make authenticated HTTPS requests to the VM's `endpoint` (returned by
   `run-microvm`/`get-microvm`) with header `X-aws-proxy-auth: <token>`. The proxy
   forwards **arbitrary request paths** and supports **streaming (SSE)**
   responses. Crucially, the token is minted by whoever holds AWS credentials —
   the **runner** — so the guest is reachable **without holding any AWS
   credentials itself**. This is the transport that lets the runner consume a
   lifecycle stream from the guest while keeping the guest credential-free.

7. **Networking is connector-based, no infrastructure to run.** Egress is
   selected with `--egress-network-connectors` (`INTERNET_EGRESS` or a VPC
   connector); ingress with `--ingress-network-connectors` (or the provided
   `NO_INGRESS`). The driver only needs **egress** to reach the C2 and GitHub —
   it connects *out*, exactly as under Docker. The managed proxy (fact 6) is a
   separate path from ingress connectors, so the VM launches with `NO_INGRESS`
   and is still reachable by the runner over the proxy.

The structural fit with `backend.Backend`: a microVM is a runner-independent
sandbox we *launch and observe* (like a container or a Firecracker VM), not a
request we hold open (like AgentCore). That is why this backend can honor the
restart-survival and graceful-stop parts of the interface contract that AgentCore
cannot.

## Design

### Overview

A new package `internal/runner/backend/lambdamicrovm` implements
`backend.Backend`. It reaches the service through `internal/x/awsmicrovm`, a
general-purpose Lambda MicroVMs client + lifecycle-hook server built on the AWS
SDK for Go v2 (credentials/region via the SDK default chain, requests SigV4-signed
with `aws/signer/v4`), plus `s3` for spec staging. Selection follows the existing
seam in `internal/command/runner.go`:

```
xagent runner --backend lambda-microvm
```

Per task, the backend:

1. Ensures a **MicroVM image** exists for the workspace (cached by image digest +
   xagent version), building it via the S3 + `create-microvm-image` flow on first
   use, or using a pre-built ARN from config.
2. Stages `spec.Files` (+ `spec.Cmd`/`spec.Env`) to **S3** and presigns a GET URL
   — the Lambda analog of the Docker backend's `CopyToContainer` tar and the
   Firecracker backend's `config.tar`. (The 16 KB run-hook payload is too small
   for an agent config with a large prompt, so it carries only a pointer.)
3. Calls `run-microvm` with the image ARN, egress connector, `NO_INGRESS`, the
   idle policy disabled (see below), `--maximum-duration-in-seconds` from the
   workspace, and a `--run-hook-payload` carrying the presigned URL. The returned
   `{microvmId, endpoint}` is tagged `xagent.task=<id>` / `xagent.runner=<id>`,
   and the runner persists it as the task's handle.

An in-image **shim** receives the `/run` hook, fetches the staged bundle,
provisions the files, and execs the driver — which connects to the C2 with its
task token exactly as under Docker. The orchestrator (`runner.Runner`), the
driver, the C2 API, the database, and the task state machine are **untouched**:
the driver already connects to the C2 by URL + token and neither knows nor cares
what launched it. That was the point of the socket-proxy elimination and
driver-owned-events prerequisites.

### Lifecycle: runner-driven termination over the auth-token proxy + SSE

The core of this revision. Because entrypoint exit does **not** stop a MicroVM
(fact 3), *some* explicit `terminate-microvm` is mandatory — but it is the
**runner's** job, not the guest's:

- **No AWS credentials in the guest.** The shim never calls `terminate-microvm`
  and the in-VM execution role does **not** grant
  `lambda-microvms:TerminateMicrovm`. IAM cannot express "terminate the VM I am
  running in": `TerminateMicrovm` takes a `microvmId`, there is no "self" concept,
  and the VM's id is only assigned at `run-microvm` time. The tightest achievable
  scope is a fleet tag-condition (e.g. `aws:ResourceTag/xagent: true`), which
  still lets compromised agent code terminate **sibling tasks'** VMs — a
  cross-task denial of service. Termination is a control-plane verb that belongs
  with the trusted runner, not inside the untrusted sandbox, mirroring how the
  Docker backend keeps the Docker socket and all teardown authority on the host.

- **The shim exposes an SSE lifecycle stream over the proxy.** The shim already
  supervises the driver (`proc.Wait()`), so it pushes lifecycle events the instant
  they happen — most importantly `driver-exited`, carrying the driver's **real
  process exit code**. The stream is served on the shim's HTTP server (port 8080,
  alongside the Lambda hooks) at an xagent-owned path (e.g.
  `GET /xagent/lifecycle`, `Accept: text/event-stream`) and is reached by the
  runner over the managed proxy. Unknown event types are ignored by the runner;
  only `driver-exited{code}` is load-bearing. (Periodic SSE comments keep the
  connection alive.)

- **The runner consumes the stream.** It mints a token with
  `CreateMicrovmAuthToken(microvmId, …, allowedPorts=[8080])` and connects to the
  VM's `endpoint` with the `X-aws-proxy-auth` header. On `driver-exited{code}` it
  records the **true exit code** and terminates the VM with the **runner's own**
  credentials (`terminate-microvm`). This delivers real-time completion plus the
  true exit code, resolving the old `TERMINATED → exit 0` ambiguity (a clean
  completion and a `max_duration` reap both land in `TERMINATED`, so VM state
  alone cannot tell them apart — see the old Open Question #3).

- **A stream drop is NOT an exit.** A transient network glitch, the proxy's
  connection-age cap, or token expiry must never fail a healthy task — and a false
  failure would *stick*, because the task is non-terminal until the driver owns its
  terminal event. So on a drop the runner does **not** emit an exit. It reconnects
  with backoff (re-minting the token if it expired) **and** consults the control
  plane via `GetMicrovm`. The disambiguation:

  | `GetMicrovm` after a drop | Action |
  |---|---|
  | `RUNNING` / `SUSPENDED` | VM is alive — reconnect the SSE stream, **no exit emitted** |
  | `TERMINATED` / `FAILED` / not-found | VM is gone — **emit an exit**, with the code from the last SSE event if one was seen, else `-1` ("report lost") |

  Only a **positive** signal emits an exit: (a) an SSE `driver-exited{code}` event,
  or (b) the control plane reporting a terminal/absent VM. The **control plane is
  the liveness authority**; SSE is the fast, exit-code-rich layer on top of it. A
  periodic `ListMicrovms` reconcile (tag-filtered to this runner) is the final
  backstop, catching VMs that went terminal while the stream was down and the
  runner never reconnected.

- **`max_duration_seconds` is a coarse backstop**, not the primary reaper. It
  covers the case where the runner itself is down and never reconnects to
  terminate a finished VM: the VM is reaped at `--maximum-duration-in-seconds`
  rather than billing indefinitely. Normal completion is runner-driven and prompt.

### The in-image shim and image contract

Lambda MicroVMs require the application to be an HTTP server exposing the
lifecycle hooks, and there is no tar-copy file-injection phase. So, like the
AgentCore backend bakes the binary in and runs `xagent tool agentcore-shim`, and
the Firecracker backend boots `xagent tool vm-init` as PID 1, the MicroVM image
bakes the xagent binary in and runs a new hidden subcommand as its application:

```
xagent tool microvm-shim      # beside `tool agent-mcp`, `tool vm-init`, `tool agentcore-shim`
```

`microvm-shim` (`internal/microvmshim`) is a minimal HTTP server (listening on
Lambda's default port 8080) that serves both the Lambda lifecycle hooks under
`/aws/lambda-microvms/runtime/v1/` (via `awsmicrovm.Handler`) and the xagent SSE
lifecycle stream:

- **`POST /run`** — decode the run-hook payload, fetch the staged bundle from its
  presigned S3 URL, provision `spec.Files` if the sandbox is fresh, then spawn the
  driver (`spec.Cmd` + `spec.Env`) **in the background** and return HTTP 200
  promptly. (`/run` must return for the VM to finish starting; the driver is
  long-running.) The shim **supervises** the spawned driver — but it does not own
  the VM's lifecycle; the runner does.
- **`POST /terminate`** — send SIGTERM to the driver, wait a grace period, then
  SIGKILL. This is the in-microVM mirror of the Docker backend's SIGTERM→SIGKILL.
  Lambda fires it on `terminate-microvm` *before* resources are released, and the
  runner also reaches it over the proxy to request a graceful stop (see `Signal`
  below), so the driver gets to catch the signal and own its terminal report.
- **`GET /xagent/lifecycle`** — the SSE stream described above. Emits
  `driver-exited{code}` when `proc.Wait()` returns, plus optional `started` /
  keep-alive events. The runner is the only consumer, over the managed proxy.
- **`POST /suspend` / `POST /resume`** — wired as near-no-ops. Idle-driven
  suspend/resume is out of scope (see "Idle policy and suspend/resume"); the hooks
  exist so the contract is satisfied.

The shim holds **no AWS credentials** and makes **no** control-plane calls — it
only supervises the driver and reports over the SSE stream. There is no
`supervise() → TerminateMicrovm` path anymore.

The staged bundle is the shim's equivalent of the Firecracker boot manifest,
carrying exactly what `backend.Spec` holds:

```go
type bundle struct {
	Cmd        []string       `json:"cmd"`         // spec.Cmd
	Env        []string       `json:"env"`         // ws.LambdaMicroVM env + spec.Env
	Files      []backend.File `json:"files"`       // spec.Files (Data base64)
	WorkingDir string         `json:"working_dir,omitempty"`
	User       string         `json:"user,omitempty"`
}
```

Files are provisioned only on a fresh sandbox and skipped on a resumed VM (gated
by a `/xagent/.provisioned` marker), reproducing the Docker backend's
provision-at-create-only semantics so a resume never clobbers the driver's
`SetupCommandsCompleted`/`Started` markers in `agent.ConfigPath(taskID)`.

Because the application must be the shim and the binary must be pre-baked,
**MicroVM images are purpose-built** (the same portability cost AgentCore
accepts, and unlike Docker/Firecracker which consume an unmodified image). The
image is `<workspace base image>` + the host-arch xagent binary at
`backend.BinaryPath` + an application that runs `xagent tool microvm-shim`,
expressed as the `Dockerfile` that `create-microvm-image` builds. xagent ships a
base image and a `Dockerfile` fragment to make this a short build; full
auto-build/push is an open question. The image cache key includes the xagent
version, since the image embeds the driver binary (same reasoning as the
Firecracker rootfs and AgentCore runtime caches).

### Workspace config

Per the backend-interface proposal, backends get sibling config sections.
`workspace.Workspace` gains a `lambda_microvm:` section next to `container:` and
the proposed `firecracker:` / `agent_core:`:

```yaml
workspaces:
  pets-workshop:
    lambda_microvm:
      image_identifier: arn:aws:lambda:us-east-1:123456789012:microvm-image/xagent-pets   # optional pre-built image
      image_source: ghcr.io/icholy/xagent-workspace-debian:latest                          # build from this if image_identifier unset
      region: us-east-1                 # default: AWS SDK resolution
      execution_role: arn:aws:iam::123456789012:role/xagent-microvm
      egress_connector: arn:aws:lambda:us-east-1:aws:network-connector:aws-network-connector:INTERNET_EGRESS
      staging_bucket: my-xagent-staging  # S3 bucket for the spec bundle
      max_duration_seconds: 14400        # default 14400 (4h), max 28800
      environment:
        CLAUDE_CODE_OAUTH_TOKEN: ${env:CLAUDE_CODE_OAUTH_TOKEN}
    agent:
      type: claude
      ...
```

```go
type LambdaMicroVM struct {
	ImageIdentifier    string            `yaml:"image_identifier"`     // pre-built MicroVM image ARN (optional)
	ImageSource        string            `yaml:"image_source"`         // OCI image to build from if ImageIdentifier unset
	Region             string            `yaml:"region"`
	ExecutionRole      string            `yaml:"execution_role"`       // IAM role the microVM assumes
	EgressConnector    string            `yaml:"egress_connector"`     // INTERNET_EGRESS or a VPC connector ARN
	StagingBucket      string            `yaml:"staging_bucket"`       // S3 bucket for the spec bundle
	MaxDurationSeconds int64             `yaml:"max_duration_seconds"` // run-microvm --maximum-duration-in-seconds
	Environment        map[string]string `yaml:"environment"`
}
```

The in-VM execution role is now **read-mostly**: it needs S3 read for the staged
bundle and whatever the workload itself requires, but **not**
`lambda-microvms:TerminateMicrovm` (or any other MicroVM control-plane verb) —
that authority lives only with the runner's credentials.

`agent:`, `commands:`, and `capabilities:` stay backend-agnostic. AWS
credentials are not in `workspaces.yaml`; they resolve through the standard AWS
SDK credential chain on the runner (env, shared config, instance/IRSA role), so
the same expansion-free config is safe to share across a heterogeneous fleet. A
workspace may set `container:`, `firecracker:`, `agent_core:`, and
`lambda_microvm:` together so one `workspaces.yaml` serves runners with different
backends.

This builds on the validation method the Firecracker proposal adds to `Backend`:

```go
type Backend interface {
	// ValidateWorkspace checks the workspace's config section for this
	// backend. The runner validates at startup and registers only the
	// workspaces its backend accepts; Launch re-validates.
	ValidateWorkspace(ws *workspace.Workspace) error
	// ... existing methods unchanged
}
```

The backend's `ValidateWorkspace` requires `execution_role`, `staging_bucket`,
`egress_connector`, and one of `image_identifier` / `image_source`, and bounds
`max_duration_seconds` to (0, 28800]. `RegisterWorkspaces` skips (with a warning)
workspaces that fail validation, so a shared `workspaces.yaml` advertises each
workspace only from runners that can run it. The `container.image is required`
check moves into the Docker backend, as the Firecracker proposal already
establishes.

### Task state and the Handle

This backend follows the shared-runner-taskstate design
(proposals/draft/shared-runner-taskstate.md, now merged): the **runner owns the
`taskstate` store** and is its only writer, and the backend does runtime work only
over opaque `backend.Handle`s. The backend persists, discovers, and reconciles
**nothing** locally — there is no per-backend state directory.

The handle's index id (`Handle.ID`, the reverse-index key the runner resolves
back to a task) is the **`microvmId`**. `Handle.Data` carries what the backend
needs to reach and clean up the VM but not for identity — most importantly the VM
**`endpoint`**, so the runner can reach the proxy (mint a token, open the SSE
stream, POST `/terminate`) and **reconnect** after a stream drop without first
re-listing:

```go
// stored opaque in Handle.Data (taskstate.Record.Data), never decoded by the store
type handleData struct {
	Endpoint    string `json:"endpoint"`      // VM proxy endpoint, for SSE + /terminate
	ImageARN    string `json:"image_arn"`
	StageBucket string `json:"stage_bucket"`  // staged spec bundle, cleaned on Destroy
	StageKey    string `json:"stage_key"`
}
```

Because the `microvmId` is the handle id (and is tagged on the VM), a restarted
runner re-adopts a task's microVM from the `taskstate` store — and, for VMs the
store somehow missed, by enumerating `ListMicrovms` filtered to its runner tag.
This is the analog of how the Docker backend re-adopts containers by id, but it
holds only a handle — no rootfs, no networking, no local process. `GetMicrovm` /
`ListMicrovms` also return the `endpoint`, so the control plane remains the
authoritative refresh for it.

### Backend method mapping

The backend implements the runtime-only interface; all task↔handle persistence is
the runner's.

| Method | Implementation |
|---|---|
| `ValidateWorkspace` | Require `execution_role`, `staging_bucket`, `egress_connector`, and `image_identifier` or `image_source`; bound `max_duration_seconds`. |
| `Launch` | Ensure the MicroVM image ARN for the workspace (use `image_identifier`, else build from `image_source` via S3 zip + `create-microvm-image`, cached by digest+version). Build the bundle from `spec`, upload to `s3://<staging_bucket>/<runner>/<task>.json`, presign a GET URL. `run-microvm` with the image ARN, `NO_INGRESS`, the egress connector, idle policy disabled, `--maximum-duration-in-seconds`, and the presigned URL as `--run-hook-payload`. Tag the returned VM (`xagent.task`, `xagent.runner`). Return the `Handle` (id = `microvmId`, `Data` = endpoint + image + staging) for the runner to persist. If `reuse` is non-nil, adopt the identified VM instead of launching fresh. |
| `Probe` | `GetMicrovm` on the handle id. `RUNNING`/`SUSPENDED` → `StateRunning`; `TERMINATED`/`FAILED`/not-found → `StateExited`. The VM state is the authority. |
| `Signal` | Graceful stop: over the managed proxy, POST the shim's `/terminate` hook (SIGTERM → grace → SIGKILL the driver). The driver catches SIGTERM and owns its terminal report to the C2; the shim emits `driver-exited` over SSE. Returns `signalled=true` if a running VM was reached. Does **not** itself `terminate-microvm` — that is `Destroy`'s job. |
| `Destroy` | `terminate-microvm` on the handle id (idempotent; also fires `/terminate` as a final SIGTERM backstop if `Signal` wasn't called), then delete the staged S3 object. Destroying an absent/already-terminated VM is not an error. |
| `Watch` | Maintain a per-VM SSE stream for each of this runner's tracked VMs (discovered via `ListMicrovms` tag-filtered to the runner, endpoints from the listing): mint a token, connect, and on `driver-exited{code}` call `handle(HandleExit{ID: microvmId, ExitCode: code})`. On a stream drop, arbitrate via `GetMicrovm` — reconnect if alive, emit an exit only if terminal/gone (last SSE code, else `-1`). A periodic `ListMicrovms` sweep is the reconcile backstop for VMs that went terminal while disconnected. Emits no exit on a bare drop. |
| `Close` | Stop the SSE streams and the reconcile sweep; leave microVMs running — they outlive the runner, exactly as containers do today. |

**Exit-code fidelity.** With the SSE `driver-exited{code}` event, the runner now
observes the driver's **true** process exit code in real time, not just the VM's
terminal state — closing the old ambiguity where a clean completion and a
`max_duration` reap both presented as `TERMINATED`. The driver-owned-events
invariant still governs correctness: the driver reports its terminal status
directly to the C2, and the runner's `Reconcile` treats an exited sandbox whose
task is still `RUNNING` as a lost report (`failed`), regardless of code. When no
SSE event was seen and the control plane shows the VM terminal/gone, the runner
reports `-1` ("report lost") and lets the state machine's status guard in
`internal/model/task.go` reconcile — the same fallback AgentCore relies on, but
exercised less often here because runner-driven termination after a `driver-exited`
event makes the clean, code-bearing path the common one.

### Idle policy and suspend/resume

Lambda MicroVMs can auto-suspend on **endpoint** idleness to cut cost while
preserving memory + disk state. This backend launches with the idle policy
**disabled**, for two reasons:

- **The runner holds an always-on SSE stream to the VM**, which is live endpoint
  traffic — so the VM is never "idle" by the traffic definition anyway, and
  reaping is **runner-driven termination**, not idle-auto-suspend. The two
  conflict: auto-suspend would race the very stream the runner uses to learn the
  task finished. Reaping a completed VM is `terminate-microvm` by the runner, full
  stop.
- **"Idle" appears to be traffic-based**, which is the wrong signal for our
  workload regardless: a CPU-busy agent that happens to receive no inbound
  endpoint traffic for a stretch would be wrongly suspended mid-work. The AWS docs
  themselves say to disable automatic suspension "for asynchronous applications
  that do not actively send or receive traffic through the endpoint."

Suspend/resume is therefore **wired but dormant**: the shim implements the
`/suspend` and `/resume` hooks (as near-no-ops) so the contract is satisfied, but
the backend never suspends on its own. Whether xagent's event-driven tasks (a
task that subscribes to a PR and sits idle awaiting a review comment) should
*explicitly* suspend the microVM between events — paying only snapshot-storage
cost while idle, then `resume-microvm` on the next routed event — is a genuinely
attractive capability unique to this backend, but it requires the orchestrator to
model "idle awaiting event" as a backend-visible state, and a CPU-vs-traffic idle
signal that does not misfire on a busy agent. That is out of scope here and called
out as an open question.

### CLI

```
xagent runner --backend lambda-microvm \
  [--lambda-microvm-region us-east-1] \
  [--lambda-microvm-reconcile 30s]
```

All flags have `XAGENT_LAMBDA_MICROVM_*` env sources; AWS credentials/region also
resolve through the standard SDK chain. The state directory is the shared
`taskstate` store's, not a backend-private one. `internal/command/runner.go`'s
backend switch gains a `lambda-microvm` case constructing
`lambdamicrovm.New(...)` with the runner id, region, the `ListMicrovms` reconcile
interval, and a logger.

`xagent download` is not extended — there is no host kernel or hypervisor binary
to fetch; the AWS SDK is compiled in. Instead, `xagent` publishes the MicroVM base
image and `Dockerfile` fragment (see image contract above).

### Package layout

```
internal/runner/
├── runner.go                 unchanged orchestrator (owns the taskstate store)
├── taskstate/                shared store (merged); the runner is the only writer
├── backend/
│   ├── backend.go            Launch/Probe/Signal/Destroy/Watch over opaque Handles
│   ├── docker/               unchanged
│   ├── firecracker/          proposed separately
│   ├── agentcore/            proposed separately
│   └── lambdamicrovm/
│       └── lambdamicrovm.go  Lambda MicroVMs implementation
└── workspace/                +LambdaMicroVM config section
internal/microvmshim/         in-VM shim: hooks + driver supervision + SSE stream
internal/x/awsmicrovm/        general-purpose client (+CreateMicrovmAuthToken, proxy helper) and Handler
internal/command/             +microvm-shim hidden subcommand
```

`internal/x/awsmicrovm` is the general-purpose service client and hook server
(modelled from the public docs; no official Go SDK yet). The two pieces this
design depends on — `CreateMicrovmAuthToken` and an authenticated proxy-request
helper (sets `X-aws-proxy-auth`, supports streaming responses) — are being added
there as available transport; the backend and the runner use them, and the shim
uses `awsmicrovm.Handler` for the hook routing.

### Testing

- Unit tests (no AWS): `ValidateWorkspace`; bundle construction (cmd/env/files
  round-trip, base64 of `File.Data`, directory entries); image cache-key
  derivation; `run-microvm` request assembly (connectors, idle policy disabled,
  payload pointer); handle construction (id = microvmId, endpoint in `Data`); the
  drop-arbitration logic (alive ⇒ reconnect/no-exit, terminal/gone ⇒ exit with
  last code / `-1`). The AWS + S3 clients sit behind small interfaces so the SDK
  calls are mocked, matching the `dockerx` moq pattern.
- The `microvm-shim` handlers are unit-tested in `internal/microvmshim` against a
  fake driver binary: `/run` payload decode + provision-once gating + background
  spawn, `/terminate` SIGTERM→SIGKILL, and the `/xagent/lifecycle` SSE stream
  emitting `driver-exited{code}` with the fake's real exit code. There is **no**
  self-terminate path to test (and a test asserts the shim makes no control-plane
  call).
- `Watch` is tested against an httptest SSE server plus a fake control plane:
  clean `driver-exited` → `HandleExit`; mid-stream drop with the VM still `RUNNING`
  → reconnect, no exit; drop with the VM `TERMINATED` → one exit with the last
  code; `max_duration` reap (VM `TERMINATED`, no SSE event) → `-1` via the
  `ListMicrovms` reconcile sweep.
- Integration tests in `backend/lambdamicrovm`, skipped unless AWS credentials, a
  test execution role, and a staging bucket are present (an env guard, mirroring
  how the Docker e2e tests require a daemon, Firecracker requires `/dev/kvm`, and
  AgentCore requires AWS creds). They cover image build, run→`driver-exited`→
  runner-terminate, provision-once-across-restart, graceful stop via `Signal`, and
  re-adoption after a simulated runner restart.
- The orchestrator needs no new tests: it already runs against `BackendMock`.

### What doesn't change

The orchestrator (`runner.go`), `EventQueue`, proto definitions, database schema,
driver, and task state machine are untouched. The Docker, Firecracker, and
AgentCore backends are unaffected beyond the shared `ValidateWorkspace` addition.
`prebuilt` is reused as-is to source the host-arch driver binary baked into the
image.

## Comparison with the sibling backends

| | Docker | Firecracker | AgentCore | **Lambda MicroVMs** |
|---|---|---|---|---|
| Host owns hypervisor | n/a | **yes** (`/dev/kvm`, kernel, TAP/NAT) | no | **no** |
| Runner-owned compute | yes | yes | no | **no** |
| Isolation | shared kernel | per-task KVM | AWS microVM | **AWS microVM (Firecracker)** |
| Image | unmodified | unmodified | purpose-built | **purpose-built** |
| Work survives runner restart | yes (container) | yes (VM) | **uncertain** (held request) | **yes (autonomous VM, re-adopted by id)** |
| Graceful stop (SIGTERM) | yes | yes (MMDS poll) | **no** (hard teardown) | **yes (`/terminate` hook over the proxy)** |
| Exit notification | docker `die` event | poll | held-request return | **SSE `driver-exited{code}` (true exit code) + control-plane backstop** |
| Teardown authority | host (no socket in guest) | host | service | **runner only — no creds in guest** |
| File injection | tar copy | config disk | invocation payload | **S3 bundle + presigned URL** |

Lambda MicroVMs is the only managed option that keeps **both** restart-survival
and graceful stop, because microVMs run autonomously (not behind a held request)
and expose a pre-termination lifecycle hook — and it does so without putting any
control-plane credential inside the sandbox.

## Trade-offs

**Runner-driven termination vs. in-guest self-termination.** The earlier design
had the shim terminate its own VM on driver exit, which needed
`TerminateMicrovm` in the (untrusted) execution role. Per #1081 that grant cannot
be scoped to "self" — only to xagent's fleet — so it hands compromised agent code
a cross-task DoS, and it lets a buggy/compromised guest manufacture a "clean exit"
at any moment. Moving termination to the trusted runner removes the credential
from the guest entirely and tightens the clean-exit signal to a real
`driver-exited` event. The cost is that the runner must learn *when* the driver
finished — which Docker gets for free (the container self-exits) but Lambda does
not — hence the SSE stream + control-plane arbitration below.

**SSE notification + control-plane authority vs. poll-only.** Watching VM state
alone (the old plan) can't see a driver exit (the VM stays `RUNNING`) and can't
tell a clean reap from a `max_duration` reap. An SSE `driver-exited{code}` stream
gives prompt completion and the true exit code; making the **control plane the
liveness authority** (and treating a stream drop as "consult `GetMicrovm`", never
as an exit) keeps a flaky proxy from failing healthy tasks. The cost is the
reconnect/backoff/token-refresh machinery and a periodic `ListMicrovms` reconcile
backstop — more moving parts than a single poll loop, but the only way to get both
fidelity and robustness.

**HTTP shim in the image vs. teaching the driver the hook contract.** The driver
could itself implement the hooks and the SSE stream. Keeping a separate
`microvm-shim` subcommand keeps the driver runtime-agnostic (still just `exec`'d
with cmd/env, identically across all backends) and confines the Lambda contract
to one small, testable place — the same reasoning that made `vm-init` and
`agentcore-shim` separate subcommands.

**S3-staged bundle vs. inlining files in the run-hook payload.** The run-hook
payload caps at 16 KB, which a real agent config (with a large prompt and MCP
server definitions) can exceed. Staging the bundle in S3 and passing a presigned
URL removes the size limit, reuses the exact `spec.Files` content, and keeps the
backend self-contained (no new C2 RPC to fetch config). The cost is an S3
dependency and a short-lived presigned URL per task — cheap and operationally
familiar in an AWS deployment that is already running microVMs.

**Persistent server is mandated by the hook contract, not by suspend/resume.** An
earlier framing justified the long-lived shim server by suspend/resume; #1081
corrected that. The server is required because `/run` is the **only** channel the
per-task spec arrives on and `/terminate` is the graceful-stop seam — both demand
a server that stays up for the driver's whole lifetime, independent of
suspend/resume (which is dormant) and independent of termination (now the
runner's). The server stays; only the in-guest `TerminateMicrovm` call was
removed.

**Purpose-built images vs. a universal image.** Like AgentCore, requiring
MicroVM-specific images is a real regression in `workspaces.yaml` portability
versus Docker/Firecracker. The alternative — the runner building and pushing
per-workspace images on the fly — reinvents a build/registry pipeline on the
runner exactly where this backend was meant to remove host compute. Shipping a
base image + `Dockerfile` fragment keeps the runner thin; auto-build is left as an
open question. Note Lambda's build step is heavier than ECR push: zip → S3 →
`create-microvm-image` (which runs the `Dockerfile` and snapshots), so the image
cache matters more.

**One MicroVM image per (workspace image, version) vs. one shared image.** A
single shared image can't carry per-workspace toolchains, and a MicroVM image
binds to one snapshot. Caching an image ARN per image digest mirrors the
Firecracker base-rootfs and AgentCore runtime caches and keeps
`create-microvm-image` off the hot path.

**Keep the runner-side semaphore vs. defer to the platform.** Lambda enforces an
account-level memory quota across running/suspended microVMs. Keeping the
orchestrator's `safesem.Semaphore` (with `--concurrency 0` = unlimited as the
opt-out) preserves a uniform client-side throttle and avoids special-casing the
orchestrator, matching the interface proposal's lean — and gives the operator a
knob to stay under the account quota and surface `ServiceQuotaExceededException`
as backpressure rather than a hard `Launch` failure.

## Open Questions

1. **Image build/push automation.** Should the release pipeline build and publish
   MicroVM-ready workspace images (zip + `create-microvm-image`), or should the
   runner build them on demand, or should operators own that entirely? The
   proposal assumes operator-built images (or `image_source` build-on-first-use) +
   a published base. The zip→S3→`create-microvm-image` build is slow, so on-demand
   build needs careful caching.
2. **Explicit suspend/resume for idle event-driven tasks.** Should a task that is
   idle awaiting a routed event (subscribed PR/issue) explicitly `suspend-microvm`
   to pay only snapshot storage, then `resume-microvm` (or auto-resume) when the
   event arrives? This is a capability unique to this backend but needs (a) the
   orchestrator to model "idle awaiting event" as a backend-visible state, and
   (b) an idle signal that tracks *work*, not *endpoint traffic* — the always-on
   SSE stream makes the VM perpetually "non-idle" by traffic, and a CPU-busy agent
   with no inbound traffic would be wrongly suspended. A cross-cutting change
   beyond a single backend; worth a follow-up.
3. **Token lifetime and proxy connection caps.** `CreateMicrovmAuthToken`'s
   `expirationInMinutes` and the proxy's connection-age cap force periodic
   re-mint + reconnect on the SSE stream. What expiration balances re-mint churn
   against blast radius if a token leaks, and does the proxy impose a hard
   max-connection age we must design the reconnect cadence around? The drop≠exit
   logic makes reconnects safe, but the cadence is a tuning question.
4. **Tag-based discovery vs. the taskstate store.** `Watch` and post-restart
   re-adoption enumerate `ListMicrovms` filtered to the runner tag; the `taskstate`
   store is the primary task↔handle source. If tag-filtered listing turns out
   unavailable (current docs filter by image, not arbitrary tag), is per-image
   listing + the store's id index enough, or do we need the C2 to persist
   `taskID ↔ microvmId`?
5. **Region / multi-region.** One backend instance targets one region
   (`--lambda-microvm-region`). Is single-region per runner acceptable, or should a
   workspace pin its own region (the `region` field allows it, but the staging
   bucket and quotas are then per-region too)?
6. **Cost and quotas.** Per-microVM compute + `run-microvm`/snapshot storage are
   billed and quota-limited per account/region. Should the backend surface
   `ServiceQuotaExceededException`/`ThrottlingException` as a distinct retryable
   backpressure condition to the orchestrator, or treat them as generic `Launch`
   failures (with exponential backoff, as the AWS docs recommend)?
