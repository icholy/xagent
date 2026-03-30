#!/bin/bash
set -e

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
