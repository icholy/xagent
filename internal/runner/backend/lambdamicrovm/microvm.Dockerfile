# Example Dockerfile for a Lambda MicroVMs image whose application is the xagent
# shim. Package this with `create-microvm-image` (zip -> S3 -> create) to produce
# the image_identifier ARN a workspace references.
#
# The base image carries the agent toolchain (claude CLI, git, language
# runtimes) exactly like a Docker-backend workspace image.
FROM ghcr.io/icholy/xagent-workspace-debian:latest

# The driver binary must live at backend.BinaryPath. Provide a host-arch
# xagent binary alongside this Dockerfile when building the image.
COPY xagent /usr/local/bin/xagent
RUN chmod 0755 /usr/local/bin/xagent

# The shim serves the MicroVM lifecycle hooks on port 8080, fetches the task
# spec on /run, and supervises the driver.
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/xagent", "tool", "microvm-shim"]
