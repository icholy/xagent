# Lambda MicroVMs backend

Runs each task in an AWS-managed Lambda MicroVM (Firecracker) instead of a Docker
container. See `proposals/implemented/lambda-microvm-backend.md` for the full
design.

```
xagent runner --backend lambda-microvm --lambda-microvm-region us-east-1
```

The runner resolves AWS credentials from the standard environment (an
instance/IRSA role in production).

## How it fits together

- The runner (`Backend`) launches a MicroVM per task with `run-microvm`, staging
  the task's spec bundle in S3 and passing a presigned GET URL as the run-hook
  payload (the 16 KB payload is too small for an agent config).
- The in-VM application is `xagent tool microvm-shim`, baked into the MicroVM
  image. On the `/run` hook it fetches the bundle, provisions the files, and runs
  the driver. On `/terminate` it SIGTERMs the driver. When the driver exits it
  self-terminates the VM.
- AWS control-plane calls go through the `Cloud`/`Stager` interfaces; the live
  implementation is in `awsmvm/`. Credentials, region resolution, and S3 staging
  use the official `aws-sdk-go-v2`. **The Lambda MicroVMs control-plane itself is
  preview** (no Go SDK yet), so `awsmvm/cloud.go` is a thin JSON client signed
  with the SDK's SigV4 signer; its wire surface is isolated there.

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

The execution role must allow the MicroVM to terminate itself
(`lambda-microvms:TerminateMicrovm`).

## Building a MicroVM image

A MicroVM image is built once per workspace (zip + `create-microvm-image`) from a
`Dockerfile` whose application is the shim. The xagent binary must be at
`/usr/local/bin/xagent` (`backend.BinaryPath`). See `microvm.Dockerfile` for an
example. `image_source` build-on-demand from the runner is a documented
follow-up; for now build the image out of band and set `image_identifier`.
