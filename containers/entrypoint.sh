#!/bin/bash
set -e

# Clean up stale pid file from previous container run
rm -f /var/run/docker.pid

# Start Docker daemon in the background
dockerd &

# Wait for Docker to be ready
for i in $(seq 1 30); do
    if docker info >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

exec "$@"
