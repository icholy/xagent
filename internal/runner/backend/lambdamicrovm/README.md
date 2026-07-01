# Lambda MicroVMs backend

Runs each task in an AWS-managed Lambda MicroVM (Firecracker) instead of a Docker
container. See `proposals/draft/lambda-microvm-backend.md` for the full design.

```
AWS_REGION=us-east-1 xagent runner --backend lambda-microvm
```

The runner resolves AWS credentials and region from the standard SDK chain
(`config.LoadDefaultConfig`: env, shared config, instance/IRSA role).

## Lifecycle (symmetric with Docker)

| | Docker | Lambda MicroVM |
|---|---|---|
| driver exits | container exits (state preserved, no cost) | **`suspend-microvm`** (snapshot-storage only, no compute, state preserved) |
| next run / restart | reuse the exited container | **`resume-microvm`** the suspended VM (driver re-spawned) |
| task archived/deleted | remove the container | **`terminate-microvm`** |

All three control-plane verbs (suspend, resume, terminate) are the **runner's**.
The guest holds **no** AWS credentials.

- The runner launches a MicroVM per task with `run-microvm`, staging the task's
  spec bundle in S3 and passing a presigned GET URL as the run-hook payload (the
  16 KB payload is too small for an agent config).
- The in-VM application is `xagent tool microvm-shim`, baked into the MicroVM
  image. On `/run` it fetches the bundle, provisions the files once, and spawns
  the driver; on `/resume` it re-spawns the driver against the preserved disk
  (no re-provision). It supervises the driver and streams `driver-exited{code}`
  on `GET /xagent/lifecycle` (SSE, sticky-replayed) over AWS's managed proxy.
- The runner's per-handle `Wait` consumes that stream (over the proxy, with a
  short-lived `CreateMicrovmAuthToken` token) and, on `driver-exited`,
  **suspends** the VM itself and returns the true exit code. A stream drop is
  arbitrated via `GetMicrovm` (the liveness authority): a running VM reconnects,
  a non-running one returns a report-lost outcome. The runner's boot-time `Load`
  probe re-attaches `Wait` to VMs still running after a restart.
- `Signal` (graceful stop) POSTs `/xagent/stop` over the proxy (SIGTERM → grace →
  SIGKILL the driver); the resulting exit suspends the VM like any completion.
- AWS control-plane calls go through the `Cloud`/`Stager` interfaces; the live
  implementation is `awsmicrovm.Client` + `awsmvm.S3Stager`. **The Lambda
  MicroVMs control plane is preview** (no Go SDK yet), so `internal/x/awsmicrovm`
  is a thin SigV4-signed JSON client; its wire surface is isolated there.

## Workspace config

```yaml
workspaces:
  example:
    lambda_microvm:
      image_identifier: arn:aws:lambda:us-east-1:123456789012:microvm-image/xagent-example
      execution_role: arn:aws:iam::123456789012:role/xagent-microvm
      egress_connector: arn:aws:lambda:us-east-1:aws:network-connector:aws-network-connector:INTERNET_EGRESS
      staging_bucket: my-xagent-staging
      max_duration_seconds: 14400
      environment:
        CLAUDE_CODE_OAUTH_TOKEN: ${env:CLAUDE_CODE_OAUTH_TOKEN}
    agent:
      type: claude
      cwd: /root
```

The in-VM **execution role is read-mostly**: it needs S3 read for the staged
bundle and whatever the workload requires, but **not**
`lambda-microvms:TerminateMicrovm` (or any other MicroVM control-plane verb) —
that authority lives only with the runner's credentials.

## Building a MicroVM image

A MicroVM image is built once per workspace (zip + `create-microvm-image`) from a
`Dockerfile` whose application is the shim. The xagent binary must be at
`/usr/local/bin/xagent` (`backend.BinaryPath`). See `microvm.Dockerfile` for an
example. `image_source` build-on-demand from the runner is a documented
follow-up; for now build the image out of band and set `image_identifier`.
