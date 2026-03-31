#!/bin/sh
set -e

# Clean up stale pid file from previous container run
rm -f /var/run/docker.pid

# Configure registry mirror if set
if [ -n "$REGISTRY_MIRROR" ]; then
    mkdir -p /etc/docker
    echo "{\"registry-mirrors\": [\"$REGISTRY_MIRROR\"]}" > /etc/docker/daemon.json
fi

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
