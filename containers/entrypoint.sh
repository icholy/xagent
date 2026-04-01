#!/bin/bash
set -e

# Configure .npmrc if NPM_TOKEN is set
if [ -n "$NPM_TOKEN" ]; then
    echo "//registry.npmjs.org/:_authToken=$NPM_TOKEN" > "$HOME/.npmrc"
fi

# Configure registry mirror if set
if [ -n "$REGISTRY_MIRROR" ]; then
    mkdir -p /etc/docker
    echo "{\"registry-mirrors\": [\"$REGISTRY_MIRROR\"]}" > /etc/docker/daemon.json
fi

# Start Docker daemon in the background if enabled
if [ -n "$ENABLE_DOCKERD" ]; then

    # Clean up stale pid file from previous container run
    rm -f /var/run/docker.pid

    # Start DockerD in background
    dockerd &

    # Wait for Docker to be ready
    for i in $(seq 1 30); do
        if docker info >/dev/null 2>&1; then
            break
        fi
        sleep 1
    done
fi

exec "$@"
