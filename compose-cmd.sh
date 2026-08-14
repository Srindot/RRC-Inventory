#!/bin/bash

# Shared Docker Compose setup, sourced by start / stop / restart / status / logs.
#
# This block used to be copied into each script, and the copies drifted: logs.sh
# never learned the docker-compose v1 fallback, so it failed outright on any
# machine without the v2 plugin while the others worked fine. One copy, one
# behaviour.

# Act on the repo rather than on whatever directory the script was called from.
cd "$(dirname "${BASH_SOURCE[0]}")" || exit 1

# Support both v2 ("docker compose") and v1 ("docker-compose")
if docker compose version &> /dev/null; then
    COMPOSE_CMD="docker compose"
elif docker-compose --version &> /dev/null; then
    COMPOSE_CMD="docker-compose"
else
    echo "❌ docker compose is not installed. Please install it first."
    exit 1
fi

# Check Docker permissions and set command accordingly
if ! docker ps &> /dev/null; then
    if groups "$USER" | grep -q docker; then
        echo "⚠️  Using sudo for Docker commands (session needs refresh)"
        DOCKER_COMPOSE_CMD="sudo $COMPOSE_CMD"
    else
        echo "❌ Docker permission error. Please run: sudo usermod -aG docker \$USER"
        echo "   (If Docker is installed but not running: sudo systemctl start docker)"
        exit 1
    fi
else
    DOCKER_COMPOSE_CMD="$COMPOSE_CMD"
fi

# compose_recreate brings services up, rebuilding first when asked.
#
# docker-compose v1.29 dies with "KeyError: 'ContainerConfig'" when it recreates
# a container whose image was just rebuilt - modern Docker no longer returns the
# field it expects. Removing the containers first skips that code path. Named
# volumes are untouched by rm, so the database and item photos survive.
compose_recreate() {
    local rebuild="$1"

    if [ "$rebuild" = "true" ]; then
        echo "🔨 Rebuilding images from the current source..."
        $DOCKER_COMPOSE_CMD build || return 1

        if [ "$COMPOSE_CMD" = "docker-compose" ]; then
            $DOCKER_COMPOSE_CMD rm -sf
        fi
    fi

    $DOCKER_COMPOSE_CMD up -d
}
