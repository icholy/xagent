# AWS Lambda MicroVMs Backend for the Runner

Issue: https://github.com/icholy/xagent/issues/1048

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
autonomously**: `run-microvm` returns a `microvmId` and the VM keeps running
independently of the caller, and AWS exposes **lifecycle hooks** (`/run`,
`/suspend`, `/resume`, `/terminate`) that let us deliver a graceful in-microVM
stop. So a task's sandbox **survives a runner restart** (re-adopted by id, like a
container that outlives the runner) and `Stop` can SIGTERM the driver — closing
the two biggest gaps in the AgentCore design — while keeping AgentCore's "no host
to run" property.

This proposal adds a `lambda-microvm` backend implementing `backend.Backend`:
AWS-managed, hardware-isolated microVMs per task with no runner-owned compute,
hypervisor, or scheduler.

## Background: the Lambda MicroVMs contract

Five facts about Lambda MicroVMs drive the design.

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
   suspended or terminated. There is no caller-held connection to keep the work
   alive — the decisive difference from AgentCore.

3. **Per-VM config is delivered via the run hook payload.** `run-microvm`
   `--run-hook-payload` accepts a **≤16 KB string** that Lambda delivers (with the
   injected `microvmId`) to the application's `POST /aws/lambda-microvms/runtime/v1/run`
   hook. Traffic to the endpoint begins only after `/run` returns HTTP 200.

4. **Lifecycle is run / suspend / resume / terminate, with hooks.** The
   application exposes HTTP hooks Lambda calls at each transition. The
   `/terminate` hook runs *before* resources are released — our seam for a
   graceful SIGTERM. `--maximum-duration-in-seconds` (≤28,800 = 8 h) caps total
   lifetime; `list-microvms` enumerates VMs (filterable by image).

5. **Networking is connector-based, no infrastructure to run.** Egress is
   selected with `--egress-network-connectors` (`INTERNET_EGRESS` or a VPC
   connector); ingress with `--ingress-network-connectors` (or the provided
   `NO_INGRESS`). The driver only needs **egress** to reach the C2 and GitHub —
   it connects *out*, exactly as under Docker.

The structural fit with `backend.Backend`: a microVM is a runner-independent
sandbox we *launch and observe* (like a container or a Firecracker VM), not a
request we hold open (like AgentCore). That is why this backend can honor the
restart-survival and graceful-stop parts of the interface contract that AgentCore
cannot.

## Design

### Overview

A new package `internal/runner/backend/lambdamicrovm` implements
`backend.Backend`. Credentials, region, and S3 use `aws-sdk-go-v2`
(`config` + `service/s3`); the Lambda MicroVMs control plane has no Go SDK yet,
so it is a thin JSON client signed with the SDK's `aws/signer/v4`, in a
general-purpose `internal/x/awsmicrovm` package that models the service with no
xagent knowledge (the backend consumes it through a `Cloud` interface).
Selection follows the existing seam in `internal/command/runner.go`:

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
3. Calls `run-microvm` with the image ARN, egress connector, `NO_INGRESS`, a
   disabled idle policy (see below), `--maximum-duration-in-seconds` from the
   workspace, and a `--run-hook-payload` carrying the presigned URL. The returned
   `microvmId` is tagged `xagent.task=<id>` / `xagent.runner=<id>` and returned to
   the runner as the `Handle.ID`, which the runner records in the shared store.

An in-image **shim** receives the `/run` hook, fetches the staged bundle,
provisions the files, and execs the driver — which connects to the C2 with its
task token exactly as under Docker. The orchestrator (`runner.Runner`), the
driver, the C2 API, the database, and the task state machine are **untouched**:
the driver already connects to the C2 by URL + token and neither knows nor cares
what launched it. That was the point of the socket-proxy elimination and
driver-owned-events prerequisites.

### The in-image shim and image contract

Lambda MicroVMs require the application to be an HTTP server exposing the
lifecycle hooks, and there is no tar-copy file-injection phase. So, like the
AgentCore backend bakes the binary in and runs `xagent tool agentcore-shim`, and
the Firecracker backend boots `xagent tool vm-init` as PID 1, the MicroVM image
bakes the xagent binary in and runs a new hidden subcommand as its application:

```
xagent tool microvm-shim      # beside `tool agent-mcp`, `tool vm-init`, `tool agentcore-shim`
```

`microvm-shim` is a minimal HTTP server (listening on Lambda's default port 8080)
implementing the MicroVM hook contract under
`/aws/lambda-microvms/runtime/v1/`:

- **`POST /run`** — decode the run-hook payload, fetch the staged bundle from its
  presigned S3 URL, provision `spec.Files` if the sandbox is fresh, then spawn the
  driver (`spec.Cmd` + `spec.Env`) **in the background** and return HTTP 200
  promptly. (The driver is long-running; `/run` must return for the VM to finish
  starting. The shim, not the driver, owns the VM lifecycle.)
- **`POST /terminate`** — send SIGTERM to the driver, wait a grace period, then
  SIGKILL. This is the in-microVM mirror of the Docker backend's
  SIGTERM→SIGKILL. Called by Lambda on `terminate-microvm` *before* resources are
  released, so the driver gets to catch the signal and own its terminal report.
- **`POST /suspend` / `POST /resume`** — flush/restore. With our run-to-completion
  model these are effectively no-ops (see "Idle policy and suspend/resume").
- When the driver **exits on its own** (task complete), the shim calls
  `terminate-microvm` on its own `microvmId` so the VM stops and billing ends —
  the in-VM equivalent of a container exiting. This requires the microVM's
  execution role to allow `lambda-microvms:TerminateMicrovm`.

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

`agent:`, `commands:`, and `capabilities:` stay backend-agnostic. AWS
credentials are not in `workspaces.yaml`; they resolve through the standard AWS
SDK credential chain on the runner (env, shared config, instance/IRSA role), so
the same expansion-free config is safe to share across a heterogeneous fleet. A
workspace may set `container:`, `firecracker:`, `agent_core:`, and
`lambda_microvm:` together so one `workspaces.yaml` serves runners with different
backends.

This uses the `ValidateWorkspace` method already on the `backend.Backend`
interface (master): the runner validates at startup and registers only the
workspaces its backend accepts; `Launch` re-validates.

The backend's `ValidateWorkspace` requires `execution_role`, `staging_bucket`,
`egress_connector`, and one of `image_identifier` / `image_source`, and bounds
`max_duration_seconds` to (0, 28800]. `RegisterWorkspaces` skips (with a warning)
workspaces that fail validation, so a shared `workspaces.yaml` advertises each
workspace only from runners that can run it. The `container.image is required`
check moves into the Docker backend, as the Firecracker proposal already
establishes.

### Task state: the shared runner store

The backend persists **nothing**. The runner owns the single, runner-local
`internal/runner/taskstate` store (`shared-runner-taskstate`) and is the only
writer: after `Launch` returns, the runner writes a `taskstate.Record{TaskID,
Type, ID, Data}` keyed by task id and reverse-indexed by handle id. The backend
only produces and consumes `backend.Handle`s.

A `lambda-microvm` handle is:

```go
const HandleType = "lambda-microvm"

// Handle.ID   = the AWS microVM id (the reverse-index key)
// Handle.Data = json of:
type handleData struct {
	ImageARN    string `json:"image_arn"`
	StageBucket string `json:"stage_bucket"`
	StageKey    string `json:"stage_key"`
}
```

The microVM id is the identity (`Handle.ID`); everything else needed only for
cleanup lives in the opaque `Handle.Data`, which the store persists and never
decodes. After a runner restart the runner re-reads its records and reconciles
each handle against `list-microvms` via `Probe` — the survival property
AgentCore lacks — with no backend-local state directory.

### Backend method mapping

The backend implements the handle-oriented interface; the runner composes
`List`/`Running`/`Remove`/`Kill`/`Start`/`Monitor` over `backend + store`.

| Method | Implementation |
|---|---|
| `ValidateWorkspace` | Require `execution_role`, `staging_bucket`, `egress_connector`, and `image_identifier`; bound `max_duration_seconds`; reject a workspace `region` that differs from the runner's. |
| `Launch(spec, reuse)` | Build the bundle from `spec`, upload to `s3://<staging_bucket>/<runner>/<task>.json`, presign a GET URL. `run-microvm` with the image ARN, `NO_INGRESS`, the egress connector, idle policy disabled, `--maximum-duration-in-seconds`, and the presigned URL as `--run-hook-payload`. If `reuse != nil`, first delete the prior run's stale staged object (decoded from `reuse.Data`) — microVMs are never adopted, only relaunched fresh. Return `Handle{Type:"lambda-microvm", ID:<microvmId>, Data:<{image_arn,stage_bucket,stage_key}>}`. |
| `Probe(h)` | Resolve `h.ID` via a single `list-microvms` lookup. `RUNNING`/`SUSPENDED` → `StateRunning`; terminal or absent → `StateExited`. |
| `Signal(h)` | Not alive → `(false, nil)`. Otherwise `terminate-microvm` (which fires the shim's `/terminate` hook → SIGTERM→SIGKILL the driver) and return `(true, nil)` — the driver owns the terminal report, same contract as Docker's SIGTERM. |
| `Destroy(h)` | Best-effort `terminate-microvm` if still alive; delete the staged S3 object (from `h.Data`). Idempotent. AWS GCs the microVM; the runner deletes the store record. |
| `Watch(fn)` | A poll loop (Lambda has no microVM event stream). Periodically `list-microvms` filtered to this runner's `xagent.runner` tag; emit `HandleExit{ID:<microvmId>, ExitCode}` once per exit (terminal state, or a VM seen alive then vanished). The runner resolves id→task via the store and ignores untracked ids. |
| `Close` | No-op; leave microVMs running — they outlive the runner, exactly as containers do today. |

Exit-code fidelity follows the interface contract. Lambda does **not** surface the
driver's process exit code through the microVM lifecycle — only the VM's terminal
*state*. So when the shim terminates the VM after a clean driver exit, the backend
observes `TERMINATED` and emits `HandleExit{ID, 0}` ("driver reported its
outcome", which it did, directly to the C2). A microVM that reaches a `FAILED`
terminal state, or vanishes without the shim having driven a clean exit, is
emitted as `HandleExit{ID, -1}` — "report lost." The runner resolves the id to a
task and, by the driver-owned-events invariant, `-1` lets the state machine's
status guard in `internal/model/task.go` reject a spurious `failed` if the driver
*did* report, or record an honest `failed` if it didn't. Correctness comes from
the state machine, not from backend fidelity — the same fallback AgentCore relies
on, but exercised less often here because the autonomous-VM model makes clean
termination the common path.

Because the microVM id is AWS-assigned and only known after `run-microvm`, the
runner's record write necessarily lags `Launch`, and there is **no deterministic
name to self-heal from** (unlike Docker's `xagent-{taskID}` container name). A
runner crash in that window orphans a running, untracked VM; the accepted
backstop is `max_duration_seconds` as the reaper (see `shared-runner-taskstate`).
There is no discovery-from-tags and no periodic reconcile beyond the shared
store's startup reconcile — a periodic reconcile is a possible follow-up.

### Idle policy and suspend/resume

Lambda MicroVMs can auto-suspend on **endpoint** idleness to cut cost while
preserving memory + disk state. xagent's driver is a **run-to-completion batch
process** that connects *out* to the C2 and exposes no ingress traffic, so
endpoint-idleness auto-suspend does not apply — the AWS docs explicitly say to
disable automatic suspension "for asynchronous applications that do not actively
send or receive traffic through the endpoint." So this backend launches with
`NO_INGRESS` and the idle policy disabled, and relies on
`--maximum-duration-in-seconds` + the shim's self-terminate-on-exit for lifetime
management.

Suspend/resume is therefore **wired but dormant**: the shim implements the
`/suspend` and `/resume` hooks (as near-no-ops) so the contract is satisfied, but
the backend never suspends on its own. Whether xagent's event-driven tasks (a
task that subscribes to a PR and sits idle awaiting a review comment) should
*explicitly* suspend the microVM between events — paying only snapshot-storage
cost while idle, then `resume-microvm` on the next routed event — is a genuinely
attractive capability unique to this backend, but it requires the orchestrator to
model "idle awaiting event" as a backend-visible state. That is out of scope here
and called out as an open question; the run-to-completion mapping above stands on
its own.

### CLI

```
AWS_REGION=us-east-1 xagent runner --backend lambda-microvm \
  [--lambda-microvm-poll 10s]
```

The runner-local store directory is the shared `--state-dir` flag (default
`/var/lib/xagent/tasks`); the backend has no state dir of its own. AWS
credentials and region resolve through the standard SDK chain
(`config.LoadDefaultConfig` — env including `AWS_REGION`, shared config,
instance/IRSA role), so there is no region flag.
`internal/command/runner.go`'s backend switch gains a `lambda-microvm` case
constructing `lambdamicrovm.New(...)` with `Cloud: awsmicrovm.NewClient(cfg)`
and `Stager: awsmvm.NewS3Stager(cfg)`.

`xagent download` is not extended — there is no host kernel or hypervisor binary
to fetch; the AWS SDK is compiled in. Instead, `xagent` publishes the MicroVM base
image and `Dockerfile` fragment (see image contract above).

### Package layout

```
internal/runner/
├── runner.go                 unchanged orchestrator (owns the shared taskstate store)
├── taskstate/                shared runner-owned store (already on master)
├── backend/
│   ├── backend.go            handle-oriented interface (already on master)
│   ├── docker/               unchanged
│   └── lambdamicrovm/
│       ├── lambdamicrovm.go  Lambda MicroVMs implementation (handle-oriented)
│       ├── client.go         Cloud / Stager interfaces
│       └── awsmvm/           xagent AWS glue: config loader + S3 Stager
internal/x/awsmicrovm/        general-purpose service client + lifecycle Handler (no xagent knowledge)
├── workspace/                +LambdaMicroVM config section
internal/microvmshim/         in-VM lifecycle-hook server
internal/command/tool.go      +microvm-shim subcommand
```

### Testing

- Unit tests (no AWS): `ValidateWorkspace`; bundle construction (cmd/env/files
  round-trip, base64 of `File.Data`, directory entries); image cache-key
  derivation; `run-microvm` request assembly (connectors, idle policy disabled,
  payload pointer); terminal-state → `Exit` mapping. The AWS + S3 clients sit
  behind small interfaces so the SDK calls are mocked, matching the `dockerx` moq
  pattern.
- The `microvm-shim` HTTP handlers are unit-tested in `internal/command` against a
  fake driver binary: `/run` payload decode + provision-once gating + background
  spawn, `/terminate` SIGTERM→SIGKILL, self-terminate-on-exit.
- Integration tests in `backend/lambdamicrovm`, skipped unless AWS credentials, a
  test execution role, and a staging bucket are present (an env guard, mirroring
  how the Docker e2e tests require a daemon, Firecracker requires `/dev/kvm`, and
  AgentCore requires AWS creds). They cover image build, run→exit,
  provision-once-across-restart, graceful stop via terminate, and re-adoption
  after a simulated runner restart.
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
| Graceful stop (SIGTERM) | yes | yes (MMDS poll) | **no** (hard teardown) | **yes (`/terminate` hook)** |
| File injection | tar copy | config disk | invocation payload | **S3 bundle + presigned URL** |

Lambda MicroVMs is the only managed option that keeps **both** restart-survival
and graceful stop, because microVMs run autonomously (not behind a held request)
and expose a pre-termination lifecycle hook.

## Trade-offs

**HTTP shim in the image vs. teaching the driver the hook contract.** The driver
could itself implement the lifecycle hooks. Keeping a separate `microvm-shim`
subcommand keeps the driver runtime-agnostic (still just `exec`'d with cmd/env,
identically across all backends) and confines the Lambda contract to one small,
testable place — the same reasoning that made `vm-init` and `agentcore-shim`
separate subcommands.

**S3-staged bundle vs. inlining files in the run-hook payload.** The run-hook
payload caps at 16 KB, which a real agent config (with a large prompt and MCP
server definitions) can exceed. Staging the bundle in S3 and passing a presigned
URL removes the size limit, reuses the exact `spec.Files` content, and keeps the
backend self-contained (no new C2 RPC to fetch config). The cost is an S3
dependency and a short-lived presigned URL per task — cheap and operationally
familiar in an AWS deployment that is already running microVMs.

**Self-terminate-on-exit vs. letting `maximum-duration` reap.** When the driver
finishes, the shim terminates its own VM immediately so billing stops, rather than
letting an idle VM run until `--maximum-duration-in-seconds`. This needs
`TerminateMicrovm` in the execution role but avoids paying for idle compute after
the task is done. `maximum-duration` remains the backstop for a shim that fails to
self-terminate.

**Purpose-built images vs. a universal image.** Like AgentCore, requiring
MicroVM-specific images is a real regression in `workspaces.yaml` portability
versus Docker/Firecracker. The alternative — the runner building and pushing
per-workspace images on the fly — reinvents a build/registry pipeline on the
runner exactly where this backend was meant to remove host compute. Shipping a
base image + `Dockerfile` fragment keeps the runner thin; auto-build is left as an
open question. Note Lambda's build step is heavier than ECR push: zip → S3 →
`create-microvm-image` (which runs the `Dockerfile` and snapshots), so the image
cache matters more.

**Lean on driver-owned-events vs. demand exit-code fidelity.** Lambda surfaces VM
*state*, not the driver's process exit code. Rather than engineer a side channel,
the design maps clean termination to `Exit{0}` and anything anomalous to
`Exit{-1}` and lets the state machine's status guard reconcile — the contract the
interface proposal already defines.

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
as backpressure rather than a hard `Start` failure.

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
   event arrives? This is a capability unique to this backend but needs the
   orchestrator to model "idle awaiting event" as a backend-visible state — a
   cross-cutting change beyond a single backend. Worth a follow-up.
3. **Exit-code fidelity.** Is `Exit{0}` on clean shim-driven termination + `-1`
   otherwise sufficient, or should the shim stash the driver's real exit code
   somewhere the backend can read post-termination (e.g. a tag or an S3 object
   written in `/terminate`) for richer diagnostics?
4. **Watch scoping.** `Watch` filters `list-microvms` to this runner's
   `xagent.runner` tag and emits every exit by id; the runner ignores untracked
   ids. Re-adoption after a restart uses the shared store's records (the
   `microvmId` is the `Handle.ID`), not a per-image scan. If `list-microvms` is
   expensive at scale, is a narrower server-side filter needed?
5. **Region / multi-region.** One backend instance targets one region (resolved
   by the SDK from `AWS_REGION`/shared config). Is single-region per runner
   acceptable, or should a workspace pin its own region (the `region` field
   allows it, but the staging bucket and quotas are then per-region too)?
6. **Cost and quotas.** Per-microVM compute + `run-microvm`/snapshot storage are
   billed and quota-limited per account/region. Should the backend surface
   `ServiceQuotaExceededException`/`ThrottlingException` as a distinct retryable
   backpressure condition to the orchestrator, or treat them as generic `Start`
   failures (with exponential backoff, as the AWS docs recommend)?
